# Device Bootstrap

Device bootstrap is the compatibility path for hardware clients. GUI and service clients should normally use the API-key provider flow instead.

## Endpoint

`POST /v1/bootstrap`

Example:

```json
{
  "device_id": "vdk-coreS3-001",
  "chip_id": "esp32s3-abcdef",
  "hardware_sku": "m5cores3-facekit-audio",
  "firmware_version": "1.2.0",
  "firmware_channel": "stable",
  "capabilities": {
    "display": "lcd",
    "touch": true,
    "usb_hid": true,
    "usb_mic": true,
    "cellular": false
  },
  "claim_token": "factory-claim-token"
}
```

Response:

```json
{
  "device_id": "vdk-coreS3-001",
  "device_token": "dtk_...",
  "ws_url": "ws://127.0.0.1:8080/v1/session/ws",
  "config_version": 1,
  "config": {
    "voice": {"enabled": true}
  }
}
```

## Token Model

The factory claim token is only used for bootstrap. It should not be used for long-running sessions.

After bootstrap, devices use the issued `device_token` for:

- WebSocket session authentication
- device config fetches
- telemetry submission

The current implementation stores hashes of issued device tokens.

## Default Demo Device

The local demo seed is intended for smoke and integration tests:

- `device_id`: `vdk-coreS3-001`
- `claim_token`: `factory-claim-token`
- `chip_id`: `esp32s3-abcdef`
- `hardware_sku`: `m5cores3-facekit-audio`

Disable the demo seed with:

```sh
AGENSENSE_DISABLE_DEMO_SEED=true go run ./cmd/agensense
```

## Security Notes

Production bootstrap needs stricter controls than the local MVP:

- short device-token TTL
- token rotation or refresh
- claim-token revocation
- audit log for bootstrap attempts
- tenant-aware claim policy
- no broad device actions before successful authentication

The current implementation is appropriate for local development and protocol validation.
