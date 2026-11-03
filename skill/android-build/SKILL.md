---
name: android-build
description: Build an Android app on the local build server so it can be downloaded and installed from the BuildDrop phone app. Use when the user asks to "build the APK", "send a build to my phone", "publish/push a build", "make a new build available", or invokes /android-build in an Android (Gradle) project.
---

# BuildDrop Android Build

Sends the current Android project's location to the local build server
(Go server in `builddrop`). The server runs Gradle, stores the APK and lists it
for the **BuildDrop** Android app, which downloads and installs it.

## Steps

1. Work out the project root: the nearest directory, starting from the current working
   directory, that contains `gradlew` or `settings.gradle(.kts)`. Usually that's the git root
   (`git rev-parse --show-toplevel`). If the user named a different project path, use that.
2. Choose the Gradle task. The default is `assembleDebug`. Use something else only if the user
   asks for it (e.g. `assembleRelease`, `:app:assembleFreeDebug`).
3. Run the helper script. It submits the build and waits until it finishes:

   ```bash
   bash <skill-directory>/scripts/submit-build.sh [--no-wait] [--force] <project-root> [task] [display-name]
   ```

   Resolve `<skill-directory>` to the directory containing this `SKILL.md`; the helper
   is bundled alongside it, independent of which agent installed the skill.

   - The server URL comes from `$ANDROID_BUILD_SERVER` (default `http://localhost:8085`).
   - If `$ANDROID_BUILD_SERVER_TOKEN` is set, it's sent as a bearer token.
   - Builds can take several minutes, so run it with a long timeout (10 minutes).
     Pass `--no-wait` to submit and return immediately.
   - If the source hasn't changed since an existing build (same commit, same uncommitted
     changes, same task), the server reuses that build instead of building again, and the
     script says so. Pass `--force` only if the user explicitly asks for a rebuild.
4. Report back to the user:
   - **Success**: project, version, branch/commit, APK size, and that it's ready to install
     from the BuildDrop app.
   - **Failure**: the script prints the tail of the Gradle log. Summarise the error. If it's a
     code problem in the current project, offer to fix it and rebuild.
   - **Server unreachable**: tell the user to start the server
     (`systemctl --user start builddrop-server`).

## API reference

- `POST /api/builds` with `{"repo_path": "/abs/path", "task": "assembleDebug", "name": "optional", "force": false}` queues a build (or returns the existing one with `"duplicate": true`)
- `GET /api/builds` lists builds, newest first
- `GET /api/builds/{id}` returns one build (`status`: queued | building | success | failed)
- `GET /api/builds/{id}/log` returns the Gradle output
- `GET /api/builds/{id}/apk` downloads the APK
- `DELETE /api/builds/{id}` deletes a build
