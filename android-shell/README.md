# Android Shell

This module packages the existing React frontend into a single Android APK.
The frontend always builds the `android` target, and the app switches between
phone and pad shells at runtime based on the current window size / orientation.

The shell is a minimal WebView wrapper. During Gradle asset merging it runs the
frontend build for the matching target and copies `image-studio/frontend/dist/`
into `app/src/main/assets/web/`.

Current scope:

- APK packaging works from the WebView shell
- Frontend startup is supported by the Android-side `AndroidImageStudio` bridge
- Desktop-only backend features that still depend on the Go/Wails runtime are
  surfaced as explicit "not implemented in Android shell yet" errors

Local build:

```bash
cd android-shell
./gradlew assembleRelease
```

Local verification without a connected device:

```bash
cd ..
node scripts/verify-local-android-shell.mjs
```

This script assembles the debug APK, checks `versionName` / `versionCode`,
verifies the APK signature, confirms the built frontend assets are embedded
into the package, and runs the Android JVM unit tests for shell-side parsing
logic.

If you already have a device or emulator attached over `adb`, you can also ask
the script to install and launch the debug APK:

```bash
IMAGE_STUDIO_ANDROID_DEVICE_SMOKE=1 node scripts/verify-local-android-shell.mjs
```

Optionally pin a specific device:

```bash
IMAGE_STUDIO_ANDROID_DEVICE_SMOKE=1 IMAGE_STUDIO_ANDROID_SERIAL=<serial> node scripts/verify-local-android-shell.mjs
```

MuMu emulator debugging:

- See `../docs/mumu-android-debug.md` for the shared ADB connection,
  Docker build, install, screenshot, rotation, and troubleshooting workflow.
