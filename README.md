# shop-sonos-noise-control

Automatically pauses and resumes Sonos speaker playback based on ambient noise levels. Designed to run on a Raspberry Pi with an analog noise sensor connected via an ADS1015 I2C ADC.

When noise exceeds a configurable dB threshold, the program pauses the target Sonos speaker. When noise drops back below the threshold, it seeks back (to account for the loud period) and resumes playback.

## Hardware Requirements

- Raspberry Pi 2+
- ADS1015 I2C ADC with an analog noise/sound level sensor
- Network connectivity to your Sonos speaker (UPnP/SSDP discovery)

## Building

Requires Go 1.23+.

```shell
# Build for the current platform:
make build

# Build for Raspberry Pi 2+ (ARM7, 32-bit):
make build-rpi

# Lint:
make lint
```

## Installation

```shell
# Install the binary:
sudo make install       # from ./out (current platform)
sudo make install-rpi   # from ./out/rpi (Raspberry Pi build)

# Install and enable the systemd service:
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable shopsonosnoisecontrol
```

After installing the systemd service, edit `/etc/systemd/system/shopsonosnoisecontrol.service` to add the desired command-line flags to `ExecStart`, then reload and start:

```shell
sudo nano /etc/systemd/system/shopsonosnoisecontrol.service
sudo systemctl daemon-reload
sudo systemctl start shopsonosnoisecontrol
```

## Configuration

All configuration is via command-line flags. Run `shopsonosnoisecontrol -help` for full usage.

### Discovering your Sonos device

Use the built-in discovery mode to find Sonos devices on your network:

```shell
shopsonosnoisecontrol -discover -interface eth0
```

This prints each device's UUID, which you'll pass via `-sonos-uuid`.

### Key flags

| Flag | Default | Description |
|---|---|---|
| `-sonos-uuid` | — | UUID of the Sonos device to control (e.g. `RINCON_347E5CF2B10401400`) |
| `-interface` | `eth0` | Network interface for UPnP discovery |
| `-db` | `75.0` | dB threshold that triggers a pause |
| `-sec` | `2` | Moving average window in seconds |
| `-extra-seekback-sec` | `1` | Extra seconds to seek back when resuming (added to the moving average window) |
| `-median` | `false` | Use moving median instead of moving average |
| `-db-filter-range` | `10-180` | dB values outside this range are discarded as invalid |
| `-verbose` | `false` | Enable detailed logging |

### Testing the Sonos connection

```shell
shopsonosnoisecontrol -test-sonos -sonos-uuid RINCON_... -interface eth0
```

### InfluxDB Metrics (optional)

Set `-metrics-device-name` to enable reporting to InfluxDB:

| Flag | Default | Description |
|---|---|---|
| `-metrics-device-name` | — | Device name tag for metrics (enables metrics when set) |
| `-metrics-influx-url` | `http://192.168.1.63:8086` | InfluxDB URL |
| `-metrics-influx-bucket` | `default` | InfluxDB bucket |
| `-metrics-influx-username` | — | InfluxDB username |
| `-metrics-influx-password` | — | InfluxDB password |

Reported metrics include `noise_level` (average and median), `noise_monitor_state`, `sonos_play_state`, and `sonos_connected`.

## License

MIT; see [LICENSE](LICENSE).
