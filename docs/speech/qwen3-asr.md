# Qwen3-ASR-1.7B Integration

## Overview

Qwen3-ASR-1.7B is Alibaba's state-of-the-art open-source speech recognition model (released Jan 2026, Apache 2.0). It replaces the previous sherpa-onnx + SenseVoiceSmall stack for both realtime and offline transcription.

| | Qwen3-ASR-1.7B | sherpa-onnx (previous) | SenseVoiceSmall (previous) |
|---|---|---|---|
| **Architecture** | Qwen3-Omni transformer | Zipformer transducer | SenseVoice encoder |
| **Parameters** | 1.7B | ~100M | ~200M |
| **WER (Open ASR)** | 5.59 | ~8-10 | ~8 |
| **Languages** | 30 + 22 Chinese dialects | zh/en | zh/en/ja/ko/yue |
| **Inference** | GPU (bfloat16) | CPU (ONNX) | GPU/CPU (FunASR) |
| **VRAM** | ~4.7 GB | 0 | ~2 GB |
| **Disk** | 4.4 GB | ~200 MB | ~80 MB |

## Quick Start

### Prerequisites

- NVIDIA GPU with ≥6 GB VRAM (RTX 5070 12GB confirmed working)
- Docker with `nvidia-container-toolkit`
- Model downloaded to `/mnt/sdc_old/models/speech/qwen3-asr-1.7b/`

### Deployment

Qwen3-ASR is managed through `localman` and Docker Compose:

```bash
# Check current status
localman --speech status

# Start services (if not running)
localman --speech start

# The CrossHare desktop service also auto-starts speech on launch
systemctl --user restart crosshare
```

### First-Time Setup

1. **Build the Docker image** (includes `qwen-asr` pip package):
   ```bash
   cd /data_store/managed/generated/apps/speech-runtime
   SPEECH_RUNTIME_REPO_ROOT=/home/jason/development/code/llm-runtime-manager \
     docker compose build stt-realtime stt-offline
   ```

2. **Download the model** (one-time, ~4.4 GB):
   ```bash
   huggingface-cli download Qwen/Qwen3-ASR-1.7B \
     --local-dir /mnt/sdc_old/models/speech/qwen3-asr-1.7b/
   ```

3. **Configure and start**:
   ```bash
   cp env/speech-qwen3asr.env env/speech.env
   SPEECH_RUNTIME_REPO_ROOT=/home/jason/development/code/llm-runtime-manager \
     docker compose up -d --wait stt-realtime
   ```

## Engine Details

### Realtime (Rolling Window)

`Qwen3ASRRollingRealtimeEngine` implements the `RealtimeRecognitionEngine` protocol using a rolling window approach:

1. PCM audio chunks are buffered
2. When enough audio accumulates (controlled by `partial_interval_ms`, default 1600ms), the buffer is written to a WAV file
3. The full WAV is transcribed via `Qwen3ASRModel.transcribe()`
4. Partial results are returned as `transcript.partial` events
5. On `commit`, a final transcription is performed

**Performance** (RTX 5070, bfloat16):
- First request (cold): ~55s model load + ~7s inference
- Subsequent requests (warm): ~0.4s for 2-3s audio

### Offline

`Qwen3ASROfflineEngine` handles file-based transcription via `POST /v1/audio/transcriptions`:

```bash
curl -F "file=@recording.wav" \
     -F "model=qwen3-asr-1.7b" \
     -F "language=zh" \
     http://127.0.0.1:8014/v1/audio/transcriptions
```

### Streaming Modes

Qwen3-ASR supports two backends via the `qwen-asr` Python package:

| Backend | Method | Streaming |
|---------|--------|-----------|
| **Transformers** (current) | `Qwen3ASRModel.from_pretrained()` + `model.transcribe()` | ❌ File-level only |
| **vLLM** (future) | `Qwen3ASRModel.LLM()` + WebSocket | ✅ True streaming via `/v1/realtime` |

The current implementation uses the Transformers backend with a rolling window. A future vLLM-based implementation would enable true incremental streaming, eliminating buffer re-decoding.

## Switching Engines

Qwen3-ASR is the default engine. To switch back to sherpa-onnx:

```bash
cd /data_store/managed/generated/apps/speech-runtime

# Switch to sherpa-onnx
cp env/speech-sherpa.env env/speech.env
SPEECH_RUNTIME_REPO_ROOT=/home/jason/development/code/llm-runtime-manager \
  docker compose up -d --force-recreate stt-realtime stt-offline
systemctl --user restart crosshare

# Switch back to Qwen3-ASR
cp env/speech-qwen3asr.env env/speech.env
SPEECH_RUNTIME_REPO_ROOT=/home/jason/development/code/llm-runtime-manager \
  docker compose up -d --force-recreate stt-realtime stt-offline
systemctl --user restart crosshare
```

### Config Files

| File | Engine | Model |
|------|--------|-------|
| `speech.env` (default) | `qwen3asr` | `qwen3-asr-1.7b` |
| `speech-qwen3asr.env` | `qwen3asr` | `qwen3-asr-1.7b` |
| `speech-sherpa.env` | `auto` (sherpa-onnx) | `sherpa-onnx-streaming-zipformer-zh-xlarge-int8-2025-06-30` |

Key env vars that control engine selection:
- `SPEECH_REALTIME_ENGINE=qwen3asr` — selects Qwen3-ASR
- `SPEECH_REALTIME_ENGINE=auto` — auto-detects (prefers sherpa-onnx if model files exist)
- `SPEECH_REALTIME_ENGINE=sherpa` — forces sherpa-onnx

## GPU Memory

Qwen3-ASR-1.7B uses ~4.7 GB VRAM per loaded instance (bfloat16 weights + CUDA context + KV cache).

**Important**: The stt-realtime and stt-offline containers each load their own model instance. To save VRAM, stop the offline container when not needed:

```bash
docker compose stop stt-offline
# Frees ~4.7 GB VRAM
```

Total VRAM with both containers: ~9.4 GB (too much for 12 GB cards with other GPU workloads).
With only realtime: ~4.7 GB.

## Troubleshooting

### Model fails to load: Triton C compiler error

```
RuntimeError: Failed to find C compiler. Please specify via CC environment variable
```

**Fix**: The Docker image needs `build-essential` for Triton JIT compilation. This is included in the current `Dockerfile.runtime`.

### WebSocket times out on first request

The model takes ~55s to load on first request. Ensure the client has a generous timeout (CrossHare's transcript client uses 10s event timeout + 3s dial timeout — the first request after a cold start may fail but subsequent ones will succeed).

### localman --speech start times out

If the Docker image needs rebuilding or the model needs downloading, `localman --speech start` may exceed its 45s timeout. Start services manually first:

```bash
cd /data_store/managed/generated/apps/speech-runtime
SPEECH_RUNTIME_REPO_ROOT=/home/jason/development/code/llm-runtime-manager \
  docker compose up -d --wait --wait-timeout 300 stt-realtime
```
