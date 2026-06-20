# Image Studio Gio Client

`gio-client/` is an independent high-performance desktop client for Windows and Linux. It uses Gio for the native GUI, reuses the existing Go request kernel from `go-cli/pkg/client`, and can act as the default `image-studio://import?...` handler on Windows / Linux.

It does not embed the React frontend, Wails, WebView2, or WebKitGTK. The current Wails desktop app remains in `image-studio/` and continues to build through the existing WebView2/WebKit path.

## Architecture

```text
gio-client/
├── cmd/image-studio-gio/      # Gio app entrypoint
├── internal/compat/           # WebView2-compatible state bridge
├── internal/ui/               # Gio immediate-mode frontend
└── internal/kernel/           # adapter around go-cli/pkg/client
```

The UI keeps the current desktop control-panel / canvas / log-rail structure, but its frontend architecture is native Gio instead of React/CSS. Request payload construction, retry behavior, SSE parsing, Images API support, proxy handling, and default model constants remain owned by `go-cli/pkg/client`.

The GUI entrypoint is built only for Windows and Linux. Other platforms compile a small unsupported stub so accidental local launches do not imply macOS support for this test client.

## Prompt Import Protocol

The prompt website can open the desktop client through:

```text
image-studio://import?token=XXXXXXXX
```

Platform ownership is split intentionally:

- macOS: handled by the Wails desktop app in `image-studio/`
- Windows / Linux: handled by `gio-client/`

The Gio client exposes protocol helpers through its CLI:

```bash
go run ./cmd/image-studio-gio protocol register
go run ./cmd/image-studio-gio protocol unregister
go run ./cmd/image-studio-gio protocol status
```

For manual Linux registration you can also use:

```bash
bash ../scripts/register-gio-linux-scheme.sh /absolute/path/to/image-studio-gio
```

When the client is not yet the default `image-studio://` handler, the first launch shows an explicit registration prompt instead of silently taking over the scheme.

## WebView2 Compatibility

Gio and the Wails/WebView2 client share a compatibility state file:

```text
<stable data root>/compat/state.json
```

It stores non-secret settings, upstream profiles, the active profile id, prompt presets, prompt history, trusted output roots, and generation history. API keys are not written to JSON; both clients use the same OS keyring service, `Image Studio`, with `api-key:profile:<profile-id>`.

On Windows the stable data root is the same registry-backed root used by WebView2, `HKCU\Software\YuanHua\Image Studio\DataRoot`, defaulting to `Documents\Image Studio`. Linux uses the user config directory at `image-studio`.

## Local Build

```bash
cd gio-client
go test ./...
go build -o /tmp/image-studio-gio ./cmd/image-studio-gio
```

Linux requires Gio's native build libraries:

```bash
sudo apt-get update
sudo apt-get install -y \
  pkg-config \
  libegl1-mesa-dev \
  libvulkan-dev \
  libwayland-dev \
  libx11-dev \
  libx11-xcb-dev \
  libxcursor-dev \
  libxfixes-dev \
  libxkbcommon-dev \
  libxkbcommon-x11-dev
```

Generated images default to the shared Image Studio output root unless the output directory field is changed.
