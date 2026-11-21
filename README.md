# anker-c200-linux

Small Linux GUI for the **Anker PowerConf C200** webcam (USB `291a:3369`). It shows a live preview next to all settings, including the vendor-only ones that AnkerWork exposes only on Windows/macOS.

| Setting | Values |
|---|---|
| Field of view | 65° / 78° / 95° |
| Microphone pickup | directional 90° / omnidirectional 360° |
| Mirror (horizontal flip) | on / off |
| HDR | on / off |
| Factory reset | restores camera defaults (asks first); the camera restarts and the app reconnects by itself |
| Standard UVC | zoom, pan, tilt, focus, exposure, white balance, brightness, contrast, saturation, sharpness, gamma, power line (anti-flicker)… (generated from what the camera reports) |

Settings are stored in the camera itself, so they stay in effect in other apps (browser, Zoom, OBS…) and survive replugging.

After every change the UI re-reads all controls from the camera. A manual control is greyed out while its automatic mode is on: exposure time with auto exposure, white balance temperature with auto WB, and focus with autofocus.

## Install

**Arch Linux (AUR):** `yay -S anker-c200` (or any AUR helper). This also adds *Anker C200 Settings* to the application menu.

**Binary:** download it from [Releases](https://github.com/upgreydd/anker-c200-linux/releases).

**From source:**

```sh
# Debian/Ubuntu deps: gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev wayland-protocols
go build -o anker-c200 .
```

## Usage

```sh
anker-c200              # auto-detects the C200
anker-c200 /dev/video2  # or pick the capture node yourself
```

Your user must be able to open `/dev/videoN`, which normally means being in the `video` group. Close other apps using the camera first, because the preview needs the stream. The camera ignores some settings, such as mirror, unless it is streaming. If the camera is unplugged or restarts, the app waits and reconnects.

## Known issues

- **Auto exposure** is the power-up mode, but the camera advertises only "Manual Mode". After switching to manual, Linux (`uvcvideo`) rejects going back to auto, and neither a USB reset nor root access helps. Only unplugging the camera and plugging it back in, or a factory reset, restores auto. The app asks before you leave auto.
- **HDR** always reads back as `1`, but writes do take effect: measured mean luma is ~135 with HDR on and ~152 with it off. Like AnkerWork, the app keeps the state itself and sends it to the camera at start.
- The C200 firmware can stop responding (USB timeouts, after which the kernel resets the device) if it gets an HDR write right after streaming starts. The app waits ~60 frames before sending one. If the camera still hangs, replug it, and the app reconnects by itself.
- The preview is YUYV 640×360, the highest uncompressed mode. Other apps still get full resolution.

## Protocol

The vendor settings use the UVC Extension Unit **6**, GUID `{41769ea2-04de-e347-8b2b-f4341aff003b}`. Each control is 60 bytes and is set with read-modify-write via `UVCIOC_CTRL_QUERY`. This was reverse-engineered from AnkerWork's `AWDeviceInterface.dll` (`AWDeviceInterfaceCamera::*`).

| Selector | Meaning | Layout |
|---|---|---|
| `0x10` | framing / FOV / mic | `[0]` mode (0 = fixed FOV), `[1]` 1 when setting FOV, `[2..3]` FOV in degrees (LE), `[5..6]` mic pickup (LE): `90` = directional, `0` = omnidirectional (factory default) |
| `0x11` | horizontal flip | `[0]` 0/1 |
| `0x12` | anti-flicker | `[0]` 0 = off, 50, 60. This is the same setting as UVC power line frequency, which the app uses instead |
| `0x13` | HDR | `[0]` 0/1, write-only in practice (always reads 1) |
| `0x0d` | device mode | `[01 01]` = factory reset, after which the camera re-enumerates on USB. Other values switch modes (DFU/recovery), and this app does not use them |

Other selectors in the DLL, such as auto-framing, AE, area metering, and sound mode, belong to other AnkerWork cameras. The C200 does not advertise them.

## License

MIT
