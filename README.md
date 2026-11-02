# Android Build Updater

Build Android projects on this laptop and install them on your phone over Tailscale.

```
Claude (android-build skill) ──POST repo path──▶ Go build server ──gradle──▶ APK
                                                     │
                       Build Updater app ◀──list / download APK (Tailscale)
```

- `server/`: Go build server (standard library only). It queues builds, runs Gradle one build at a time, and stores the APKs and logs.
- `android/`: the **Build Updater** app (Kotlin + Jetpack Compose). It lists builds, downloads them with a progress bar and installs them through `PackageInstaller`.
- `skill/android-build/`: Claude Code skill that sends the current project to the server.

## Setup

```bash
./install.sh
```

This builds the server into `~/.local/bin`, installs and starts the `android-build-server` systemd user service on `0.0.0.0:8085`, and symlinks the skill into `~/.claude/skills/android-build`.

To keep the service running while you're logged out: `loginctl enable-linger $USER`.

### Phone app

```bash
cd android && ./gradlew assembleDebug
adb install app/build/outputs/apk/debug/app-debug.apk
```

Or open `http://<tailscale-ip>:8085/api/builds` on the phone and download the APK. After that the app can update itself.
The default server URL is this machine's Tailscale IP (`http://100.77.132.108:8085`). You can change it in **Settings** in the app, or at build time with `-PdefaultServerUrl=...`.
The first time you install something, Android asks you to allow "Install unknown apps" for Build Updater.

## API

| Method | Path | Description |
|---|---|---|
| GET | `/api/health` | Health check |
| GET | `/api/builds[?status=success]` | List builds, newest first |
| POST | `/api/builds` | `{"repo_path": "/abs/path", "task": "assembleDebug", "name": "optional"}` |
| GET | `/api/builds/{id}` | Build details (`queued`, `building`, `success`, `failed`) |
| GET | `/api/builds/{id}/log` | Gradle output |
| GET | `/api/builds/{id}/apk` | Download the APK (supports Range) |
| DELETE | `/api/builds/{id}` | Delete a build |

Server flags and env vars: `-addr` / `BUILD_SERVER_ADDR`, `-data` / `BUILD_SERVER_DATA` (default `~/.local/share/android-build-server`), `-token` / `BUILD_SERVER_TOKEN` (requires `Authorization: Bearer` on POST/DELETE), `-timeout`.

## Usage from Claude

Inside any Android project, ask Claude to "send a build to my phone" or run `/android-build`.
