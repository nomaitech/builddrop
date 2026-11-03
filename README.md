# BuildDrop

*From code to phone.*

Build Android projects on your development machine and install them on your phone over Tailscale. Ask your coding agent to send a build, then download and install it in the BuildDrop app.

```text
Codex / Claude Code → android-build skill → Go server → Gradle → APK
                                               ↓
                                    BuildDrop Android app
                                      download + install
```

- **Server:** queues Gradle builds, captures logs, and serves APKs through an API and a web page. Uses the Go standard library only.
- **Android app:** Kotlin and Jetpack Compose; groups builds by project, shows build status, downloads APKs, and installs them with Android's package installer.
- **Agent skill:** self-contained instructions and submission script in [skill/android-build](skill/android-build). Reuses existing builds when sources and the Gradle task have not changed.

## Requirements

The build machine needs Linux with systemd user services, Go (see [go.mod](server/go.mod)), an Android SDK and JDK suitable for the projects being built, and Tailscale. The skill's helper script also needs Bash, curl, and Python 3. Android projects must be accessible on the build machine: the skill sends a local path, rather than uploading source code.

The phone needs Android 8.0 or newer and network access to the server, normally through the same Tailscale network.

## Install the server and skill

```bash
git clone git@github.com:nomaitech/builddrop.git
cd builddrop
./install.sh
```

The installer builds `~/.local/bin/builddrop-server`, installs and starts the `builddrop-server.service` user service on port 8085, and links the bundled skill into `${CODEX_HOME:-~/.codex}/skills/android-build` and `~/.claude/skills/android-build`. Existing skill directories are kept; existing symlinks are updated. Keep this checkout in place because the skills link to it.

To keep the server running after logout:

```bash
loginctl enable-linger "$USER"
```

Manage the server with `systemctl --user status builddrop-server` and inspect logs with `journalctl --user -u builddrop-server`.

The service listens on `0.0.0.0:8085`; network access is controlled by your machine's network and firewall configuration. An optional API token protects build creation and deletion.

## Install the phone app

```bash
cd android
./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Alternatively, after submitting a build of this app, open `http://<build-machine-tailscale-ip>:8085/` on the phone and download its APK. BuildDrop can then install future builds of itself.

Set the server URL in the app's **Settings**, or supply it when compiling:

```bash
./gradlew assembleDebug -PdefaultServerUrl=http://<build-machine-tailscale-ip>:8085
```

The checkout's default URL is `http://100.77.132.108:8085`; change it for your machine. Android asks you to allow BuildDrop to install unknown apps the first time you install a build.

## Use the bundled skill

The complete skill lives in [skill/android-build/SKILL.md](skill/android-build/SKILL.md), with its helper in [scripts/submit-build.sh](skill/android-build/scripts/submit-build.sh). It works from either agent's skill directory and can also be copied to another installation together with its `scripts` directory.

Inside an Android project, ask Codex or Claude Code to **“send a build to my phone”**, or invoke the `android-build` skill using your agent's skill invocation UI. The default Gradle task is `assembleDebug`.

You can also call the helper directly from this checkout:

```bash
./skill/android-build/scripts/submit-build.sh /absolute/path/to/project
./skill/android-build/scripts/submit-build.sh --no-wait /absolute/path/to/project assembleDebug "My app"
./skill/android-build/scripts/submit-build.sh --force /absolute/path/to/project
```

Set `ANDROID_BUILD_SERVER` to override the helper's default `http://localhost:8085`. Set `ANDROID_BUILD_SERVER_TOKEN` when the server requires a token. Without `--no-wait`, the helper waits for completion and prints build details or the failure log.

## Server configuration

| Flag | Environment variable | Default |
| --- | --- | --- |
| `-addr` | `BUILD_SERVER_ADDR` | `0.0.0.0:8085` |
| `-data` | `BUILD_SERVER_DATA` | `~/.local/share/builddrop-server` |
| `-token` | `BUILD_SERVER_TOKEN` | No token |
| `-timeout` | — | `30m` |

The installed service passes `-addr` explicitly. Use `systemctl --user edit builddrop-server` for service overrides and restart the service after changing configuration. When a token is configured, POST and DELETE requests require `Authorization: Bearer <token>`.

## API

| Method | Path | Description |
| --- | --- | --- |
| GET | `/` | Web page listing builds with download links |
| GET | `/api/health` | Health check |
| GET | `/api/builds[?status=success]` | List builds, newest first |
| POST | `/api/builds` | Queue a build or reuse an existing matching build |
| GET | `/api/builds/{id}` | Build details and status |
| GET | `/api/builds/{id}/log` | Gradle output |
| GET | `/api/builds/{id}/apk` | Download the APK; supports Range requests |
| DELETE | `/api/builds/{id}` | Delete a build |

Example build request:

```json
{"repo_path": "/absolute/path/to/project", "task": "assembleDebug", "name": "My app", "force": false}
```

Build statuses are `queued`, `building`, `success`, and `failed`. Reused builds include `"duplicate": true` in the response.

## Development

```bash
(cd server && go test ./...)
bash -n install.sh skill/android-build/scripts/submit-build.sh
(cd android && ./gradlew assembleDebug)
```

## Upgrading from Build Updater

Run `./install.sh` to stop and disable the old `android-build-server` service, install `builddrop-server`, and move existing build history to `~/.local/share/builddrop-server` when that directory does not already exist. Copy custom service overrides to the new service before upgrading; custom data locations need an explicit configuration override.

The Android application ID remains `com.jonbesga.buildupdater`, so BuildDrop can update existing installations and retain their settings. The `android-build` skill name and environment variable names remain compatible.
