# Speech Recognition Architecture

## Pipeline Overview

```
Android Phone (AudioRecord)
  │  PCM 16-bit LE, 16kHz, mono
  │  HTTP POST /api/audio/stream
  ▼
CrossHare Desktop (Go, port 8384)
  │  audio.Receiver → WAV file storage
  │  transcript.Client → WebSocket
  ▼
STT Service (Python, port 8013)
  │  /v1/audio/realtime WebSocket
  │  Engine dispatch via SPEECH_REALTIME_ENGINE
  ▼
Inference Engine
  ├── SherpaOnnxStreamingRealtimeEngine  (true streaming)
  ├── Qwen3ASRRollingRealtimeEngine      (rolling window)
  └── SenseVoiceRollingRealtimeEngine    (rolling window, fallback)
```

## Service Components

### CrossHare Desktop (`crosshare --no-tray`)

- **Port**: 8384
- **Systemd**: `crosshare.service` (user)
- **Audio receive**: `POST /api/audio/stream` — receives PCM audio from Android, writes WAV, returns transcript
- **Transcript client**: Connects to STT WebSocket at `ws://127.0.0.1:8013/v1/audio/realtime` (configurable via `CROSSHARE_TRANSCRIPT_WS`)
- **Auto-start**: On startup, calls `localman --speech start --json` to bring up STT services (timeout: 45s)
- **Text injection**: When transcript is final, injects text into focused window via `CROSSHARE_TEXT_INJECT` (GNOME Remote Desktop portal)

### STT Realtime (`llm-node-stt-realtime`)

- **Port**: 8013
- **Image**: `speech-runtime-stt-realtime` (built from `llm-node/docker/Dockerfile.runtime` target `speech-runtime`)
- **Mode**: `SPEECH_SERVICE_MODE=realtime`
- **WebSocket**: `/v1/audio/realtime` — accepts base64-encoded PCM16LE chunks, returns `transcript.partial` / `transcript.final` events

### STT Offline (`llm-node-stt-offline`)

- **Port**: 8014
- **Image**: Same as realtime
- **Mode**: `SPEECH_SERVICE_MODE=offline`
- **REST**: `POST /v1/audio/transcriptions` — file upload transcription; `POST /v1/audio/jobs` — async job API

### Gateway (optional, not required for CrossHare)

- **Port**: 8000 (Docker internal, not mapped to host)
- **Purpose**: Unified API routing, rate limiting, health aggregation
- CrossHare connects directly to port 8013, bypassing the gateway

## Engine Types

### True Streaming: SherpaOnnxStreamingRealtimeEngine

```
PCM chunk → accept_waveform() → decode_stream() → incremental text
                                      ↑
                              only new data processed
```

- Uses ONNX runtime with sherpa-onnx transducer
- **True incremental decoding** — each chunk only processes new audio
- Low latency, constant memory
- Requires sherpa-onnx model files (encoder/decoder/joiner ONNX + tokens.txt)
- Selected when `SPEECH_REALTIME_ENGINE=sherpa` or `auto` (with sherpa model available)

### Rolling Window: Qwen3ASRRollingRealtimeEngine

```
PCM chunk → buffer.append() → threshold met? → write WAV → model.transcribe() → full text
                                   ↑
                           re-decodes entire buffer each time
```

- Uses Qwen3-ASR via `qwen-asr` Python package (Transformers backend)
- **Pseudo-streaming** — re-decodes the full audio buffer each partial interval
- Higher latency for long recordings (O(n²) total computation)
- GPU-accelerated (bfloat16 on CUDA)
- Selected when `SPEECH_REALTIME_ENGINE=qwen3asr`

### Rolling Window: SenseVoiceRollingRealtimeEngine

- Same rolling window approach as Qwen3ASR, but uses FunASR SenseVoiceSmall
- Multi-language support (zh/en/ja/ko/yue)
- CPU or GPU
- Selected when `SPEECH_REALTIME_ENGINE=sensevoice` or as fallback

## WebSocket Protocol

All engines share the same WebSocket protocol at `/v1/audio/realtime`:

### Client → Server

| Event | Description |
|-------|-------------|
| `session.update` | Configure language, audio format, sample rate, channels |
| `input_audio_buffer.append` | Base64-encoded PCM16LE audio chunk |
| `input_audio_buffer.commit` | Finalize and request final transcript |
| `input_audio_buffer.clear` | Clear the audio buffer |

### Server → Client

| Event | Description |
|-------|-------------|
| `session.started` | Connection established, includes engine and model info |
| `session.updated` | Session configuration applied |
| `input_audio_buffer.appended` | Audio chunk accepted (no transcript yet) |
| `transcript.partial` | Interim transcription result with latency |
| `transcript.final` | Final transcription after commit |
| `transcript.error` | Error with message |
| `input_audio_buffer.cleared` | Buffer cleared |

### Audio Format

All engines expect:
- **Encoding**: PCM 16-bit signed integer, little-endian
- **Sample rate**: 16000 Hz
- **Channels**: 1 (mono)

## Configuration

### CrossHare

| Env Var | Default | Purpose |
|---------|---------|---------|
| `CROSSHARE_TRANSCRIPT_WS` | `ws://127.0.0.1:8013/v1/audio/realtime` | STT WebSocket URL |
| `CROSSHARE_TRANSCRIPT_LANGUAGE` | `auto` | Transcription language |
| `CROSSHARE_SPEECH_AUTOSTART` | `true` | Auto-start speech runtime on launch |
| `CROSSHARE_TEXT_INJECT` | unset | Enable text injection |
| `CROSSHARE_TEXT_INJECT_MODE` | unset | Injection mode (`portal-unicode`) |

### STT Engine

| Env Var | Default | Purpose |
|---------|---------|---------|
| `SPEECH_REALTIME_ENGINE` | `auto` | Engine: `auto`, `sherpa`, `qwen3asr`, `sensevoice` |
| `SPEECH_REALTIME_MODEL_ID` | (auto-detected) | Model ID for realtime engine |
| `SPEECH_MODEL_ID` | `sensevoice-small` | Model ID for offline engine |
| `SPEECH_SHERPA_ONNX_MODEL_DIR` | (from catalog) | Direct path to sherpa-onnx model directory |
| `SPEECH_SHERPA_ONNX_PROVIDER` | `cpu` | ONNX runtime provider (`cpu` or `cuda`) |
| `SPEECH_SHERPA_ONNX_NUM_THREADS` | `2` | CPU threads for sherpa-onnx |
| `SPEECH_ENDPOINT_SILENCE_SECONDS` | `0.8` | Trailing silence threshold for endpoint detection |
| `SPEECH_QWEN3ASR_MODEL_DIR` | `/mnt/sdc_old/models/speech/qwen3-asr-1.7b` | Qwen3-ASR model path override |
