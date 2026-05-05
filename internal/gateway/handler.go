package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/agendash/AgenSense/internal/gateway/wsconn"
	"github.com/agendash/AgenSense/internal/protocol"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/service"
	"github.com/agendash/AgenSense/internal/session"
)

// Handler serves the MVP WebSocket gateway protocol.
type Handler struct {
	control  service.ControlPlane
	pipeline *session.Pipeline
	clock    func() time.Time
}

// NewHandler constructs the WebSocket gateway handler.
func NewHandler(control service.ControlPlane, pipeline *session.Pipeline) *Handler {
	return &Handler{
		control:  control,
		pipeline: pipeline,
		clock:    time.Now,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.control == nil || h.pipeline == nil {
		slog.Error("websocket gateway unavailable")
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)
		return
	}

	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		slog.Warn("websocket connection rejected: missing bearer token",
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	deviceID := r.Header.Get("X-Device-Id")
	if deviceID == "" {
		slog.Warn("websocket connection rejected: missing device id",
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)
		http.Error(w, "missing X-Device-Id", http.StatusUnauthorized)
		return
	}

	deviceCtx, err := h.control.AuthenticateDeviceToken(r.Context(), deviceID, token)
	if err != nil {
		slog.Warn("websocket authentication failed",
			"device_id", deviceID,
			"remote_addr", r.RemoteAddr,
			"error", err,
		)
		http.Error(w, err.Error(), mapHTTPStatus(err))
		return
	}

	conn, err := wsconn.Upgrade(w, r)
	if err != nil {
		slog.Warn("websocket upgrade failed",
			"device_id", deviceID,
			"remote_addr", r.RemoteAddr,
			"error", err,
		)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()

	sessionID := fmt.Sprintf("sess-%d", h.clock().UnixNano())
	slog.Info("websocket session connected",
		"device_id", deviceCtx.DeviceID,
		"session_id", sessionID,
		"tenant_id", deviceCtx.TenantID,
		"instance_id", deviceCtx.InstanceID,
		"provider_profile_id", deviceCtx.ProviderProfileID,
	)
	streamState := gatewayStreamState{}
	helloSeen := false

	for {
		opcode, payload, err := conn.ReadFrame()
		if err != nil {
			slog.Info("websocket session closed",
				"device_id", deviceCtx.DeviceID,
				"session_id", sessionID,
				"error", err,
			)
			return
		}

		switch opcode {
		case wsconn.OpText:
			event, err := protocol.DecodeEvent(payload)
			if err != nil {
				slog.Warn("websocket event decode failed",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"error", err,
				)
				_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
					Code:    "invalid_event",
					Message: err.Error(),
				})
				continue
			}
			switch event.Type {
			case protocol.EventHello:
				var hello protocol.HelloPayload
				if err := event.DecodePayload(&hello); err != nil {
					slog.Warn("hello payload rejected",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"request_id", event.RequestID,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, event.RequestID, protocol.EventError, protocol.ErrorPayload{
						Code:    "invalid_hello",
						Message: err.Error(),
					})
					continue
				}
				helloSeen = true
				slog.Info("device hello received",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"request_id", event.RequestID,
					"reported_config_version", hello.State.ConfigVersion,
					"desired_config_version", deviceCtx.ConfigVersion,
				)
				if hello.State.ConfigVersion > 0 && hello.State.ConfigVersion != deviceCtx.ConfigVersion {
					_ = h.control.AckConfig(r.Context(), deviceCtx.DeviceID, hello.State.ConfigVersion)
				}
				if err := writeProtocolEvent(conn, sessionID, event.RequestID, protocol.EventHelloOK, protocol.HelloOKPayload{
					ServerTimeMS:         h.clock().UnixMilli(),
					DesiredConfigVersion: deviceCtx.ConfigVersion,
				}); err != nil {
					return
				}
				if err := writeProtocolEvent(conn, sessionID, "", protocol.EventConfigSnapshot, protocol.ConfigSnapshotPayload{
					ConfigVersion: deviceCtx.ConfigVersion,
					Config:        deviceCtx.Config,
				}); err != nil {
					return
				}
			case protocol.EventTelemetry:
				if err := h.control.UpdateTelemetry(r.Context(), deviceCtx.DeviceID, event.Payload); err != nil {
					slog.Warn("telemetry update failed",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "telemetry_rejected",
						Message: err.Error(),
					})
				}
			case protocol.EventAudioStart:
				if !helloSeen {
					slog.Warn("audio.start rejected before hello",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "hello_required",
						Message: "send hello before audio.start",
					})
					continue
				}
				var audioStart protocol.AudioStartPayload
				if err := event.DecodePayload(&audioStart); err != nil {
					slog.Warn("audio.start payload rejected",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "invalid_audio_start",
						Message: err.Error(),
					})
					continue
				}
				if err := streamState.Tracker.Start(audioStart); err != nil {
					slog.Warn("audio.start rejected by stream tracker",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"stream_id", audioStart.StreamID,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "stream_rejected",
						Message: err.Error(),
					})
					continue
				}
				streamState = gatewayStreamState{
					ID: audioStart.StreamID,
					Format: provider.AudioFormat{
						Codec:        fallbackString(audioStart.Codec, deviceCtx.DefaultAudioCodec),
						SampleRateHz: fallbackInt(audioStart.SampleRateHz, deviceCtx.DefaultSampleRateHz),
						Channels:     fallbackInt(audioStart.Channels, 1),
					},
					Tracker: streamState.Tracker,
				}
				slog.Debug("audio stream opened",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"stream_id", audioStart.StreamID,
					"codec", audioStart.Codec,
					"sample_rate_hz", audioStart.SampleRateHz,
					"channels", audioStart.Channels,
				)
			case protocol.EventAudioStop:
				if streamState.ID == "" {
					slog.Warn("audio.stop rejected without active stream",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "no_active_stream",
						Message: "audio.stop received without audio.start",
					})
					continue
				}
				var audioStop protocol.AudioStopPayload
				if err := event.DecodePayload(&audioStop); err != nil {
					slog.Warn("audio.stop payload rejected",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "invalid_audio_stop",
						Message: err.Error(),
					})
					continue
				}
				if err := streamState.Tracker.Stop(audioStop); err != nil {
					slog.Warn("audio.stop rejected by stream tracker",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"stream_id", audioStop.StreamID,
						"last_seq", audioStop.LastSeq,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "stream_mismatch",
						Message: err.Error(),
					})
					streamState = gatewayStreamState{}
					continue
				}
				sink := &gatewaySink{
					conn:      conn,
					sessionID: sessionID,
				}
				turnStart := time.Now()
				slog.Info("voice turn received",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"stream_id", streamState.ID,
					"audio_bytes", len(streamState.Audio),
				)
				err := h.pipeline.Run(r.Context(), session.PipelineRequest{
					SessionID: sessionID,
					DeviceID:  deviceCtx.DeviceID,
					StreamID:  streamState.ID,
					Format:    streamState.Format,
					Audio:     append([]byte(nil), streamState.Audio...),
				}, sink)
				streamState = gatewayStreamState{}
				if err != nil {
					slog.Error("voice pipeline failed",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"stream_id", audioStop.StreamID,
						"duration_ms", time.Since(turnStart).Milliseconds(),
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "pipeline_failed",
						Message: err.Error(),
					})
					continue
				}
				slog.Info("voice pipeline completed",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"stream_id", audioStop.StreamID,
					"duration_ms", time.Since(turnStart).Milliseconds(),
				)
			case protocol.EventConfigAck:
				var ack protocol.ConfigAckPayload
				if err := event.DecodePayload(&ack); err != nil {
					slog.Warn("config ack payload rejected",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "invalid_config_ack",
						Message: err.Error(),
					})
					continue
				}
				if err := h.control.AckConfig(r.Context(), deviceCtx.DeviceID, ack.ConfigVersion); err != nil {
					slog.Warn("config ack failed",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"config_version", ack.ConfigVersion,
						"error", err,
					)
					_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
						Code:    "config_ack_failed",
						Message: err.Error(),
					})
				} else {
					slog.Info("config ack received",
						"device_id", deviceCtx.DeviceID,
						"session_id", sessionID,
						"config_version", ack.ConfigVersion,
					)
				}
			default:
				slog.Warn("unsupported websocket event",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"event_type", event.Type,
				)
				_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
					Code:    "unknown_event",
					Message: "unsupported event type",
				})
			}
		case wsconn.OpBinary:
			if streamState.ID == "" {
				slog.Warn("binary audio rejected without active stream",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
				)
				_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
					Code:    "no_active_stream",
					Message: "binary audio received without audio.start",
				})
				continue
			}
			if err := streamState.Tracker.AddFrame(); err != nil {
				slog.Warn("binary audio rejected by stream tracker",
					"device_id", deviceCtx.DeviceID,
					"session_id", sessionID,
					"stream_id", streamState.ID,
					"error", err,
				)
				_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
					Code:    "stream_rejected",
					Message: err.Error(),
				})
				continue
			}
			streamState.Audio = append(streamState.Audio, payload...)
		case wsconn.OpClose:
			slog.Info("websocket close frame received",
				"device_id", deviceCtx.DeviceID,
				"session_id", sessionID,
			)
			return
		default:
			slog.Warn("unsupported websocket frame",
				"device_id", deviceCtx.DeviceID,
				"session_id", sessionID,
				"opcode", opcode,
			)
			_ = writeProtocolEvent(conn, sessionID, "", protocol.EventError, protocol.ErrorPayload{
				Code:    "unsupported_frame",
				Message: "unsupported websocket frame",
			})
		}
	}
}

type gatewayStreamState struct {
	ID      string
	Format  provider.AudioFormat
	Audio   []byte
	Tracker protocol.StreamTracker
}

type gatewaySink struct {
	conn      *wsconn.Conn
	sessionID string
	outbound  protocol.StreamTracker
}

func (s *gatewaySink) OnASRFinal(text string) error {
	slog.Info("sending asr.final",
		"session_id", s.sessionID,
		"text_chars", len(text),
		"text", text,
	)
	return writeProtocolEvent(s.conn, s.sessionID, "", protocol.EventASRFinal, protocol.ASRFinalPayload{Text: text})
}

func (s *gatewaySink) OnLLMDelta(text string) error {
	return writeProtocolEvent(s.conn, s.sessionID, "", protocol.EventLLMDelta, protocol.LLMDeltaPayload{Text: text})
}

func (s *gatewaySink) OnLLMDone(fullText string) error {
	slog.Info("sending llm.done",
		"session_id", s.sessionID,
		"text_chars", len(fullText),
		"text", fullText,
	)
	return writeProtocolEvent(s.conn, s.sessionID, "", protocol.EventLLMDone, protocol.LLMDonePayload{Text: fullText})
}

func (s *gatewaySink) OnTTSStart(streamID string, format provider.AudioFormat, text string) error {
	if err := s.outbound.Start(protocol.AudioStartPayload{
		StreamID:     streamID,
		Codec:        format.Codec,
		SampleRateHz: format.SampleRateHz,
		Channels:     format.Channels,
	}); err != nil {
		return err
	}
	slog.Info("sending tts.start",
		"session_id", s.sessionID,
		"stream_id", streamID,
		"codec", format.Codec,
		"sample_rate_hz", format.SampleRateHz,
		"channels", format.Channels,
		"text_chars", len(text),
		"text", text,
	)
	return writeProtocolEvent(s.conn, s.sessionID, "", protocol.EventTTSStart, protocol.AudioStartPayload{
		StreamID:     streamID,
		Codec:        format.Codec,
		SampleRateHz: format.SampleRateHz,
		Channels:     format.Channels,
	})
}

func (s *gatewaySink) OnTTSChunk(data []byte) error {
	if err := s.outbound.AddFrame(); err != nil {
		return err
	}
	return s.conn.WriteBinary(data)
}

func (s *gatewaySink) OnTTSStop(streamID string) error {
	_, frames, ok := s.outbound.Active()
	if !ok {
		return fmt.Errorf("tts.stop without active outbound stream")
	}
	payload := protocol.AudioStopPayload{
		StreamID: streamID,
		LastSeq:  frames,
	}
	if err := s.outbound.Stop(payload); err != nil {
		return err
	}
	slog.Info("sending tts.stop",
		"session_id", s.sessionID,
		"stream_id", streamID,
		"frame_count", frames,
	)
	return writeProtocolEvent(s.conn, s.sessionID, "", protocol.EventTTSStop, payload)
}

func (s *gatewaySink) OnAction(action session.Action) error {
	slog.Info("sending action.execute",
		"session_id", s.sessionID,
		"action_id", action.ActionID,
		"kind", action.Kind,
	)
	return writeProtocolEvent(s.conn, s.sessionID, "", protocol.EventActionExecute, protocol.ActionExecutePayload{
		ActionID: action.ActionID,
		Kind:     action.Kind,
		Payload:  action.Payload,
	})
}

func writeProtocolEvent(conn *wsconn.Conn, sessionID, requestID, eventType string, payload any) error {
	event, err := protocol.NewEvent(eventType, requestID, sessionID, payload)
	if err != nil {
		return err
	}
	data, err := protocol.MarshalEvent(event)
	if err != nil {
		return err
	}
	return conn.WriteText(data)
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func mapHTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, service.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func fallbackInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
