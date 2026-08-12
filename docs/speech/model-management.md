# Model Management

CrossHare's speech recognition models are managed through **localman** — the `llm-runtime-manager` CLI that handles model cataloging, download verification, and Docker service orchestration.

## Architecture

```
localman CLI
  │
  ├── Model Catalog (managed_model_catalog.json)
  │     └── Model entries: id, backend, file_path, hf_repo, hf_files
  │
  ├── App Templates (app_templates/speech-runtime/)
  │     ├── manage.sh           — docker compose wrapper
  │     ├── compose/            — Docker Compose config
  │     └── env/                — Environment configs
  │           ├── speech.env          — active config (default: Qwen3-ASR)
  │           ├── speech-qwen3asr.env — Qwen3-ASR config
  │           └── speech-sherpa.env   — sherpa-onnx config (rollback)
  │
  └── Generated Apps (/data_store/managed/generated/apps/speech-runtime/)
        └── Rendered copy of app template + env overrides
```

## Commands

```bash
# List all registered models
localman -m list

# Speech service management
localman --speech status          # Health check + service status
localman --speech start           # Start all speech services
localman --speech stop            # Stop all speech services
localman --speech urls            # Show API endpoint URLs

# JSON output for scripting
localman --speech status --json
localman --speech start --json
```

## Model Catalog

Speech models are registered in `managed_model_catalog.json` with `category: "speech"`. Each entry specifies:

| Field | Description | Example |
|-------|-------------|---------|
| `model_id` | Unique identifier | `qwen3-asr-1.7b` |
| `category` | Must be `"speech"` | `speech` |
| `backend` | Engine identifier | `qwen3asr`, `sherpa-onnx`, `sensevoice` |
| `filename` | Primary model file | `config.json` |
| `file_path` | Absolute path to model file | `/mnt/sdc_old/models/speech/qwen3-asr-1.7b/config.json` |
| `hf_repo` | HuggingFace repository | `Qwen/Qwen3-ASR-1.7B` |
| `hf_files` | (optional) List of required files for multi-file models | `["tokens.txt", "encoder.onnx", ...]` |

### Registered Speech Models

| Model ID | Backend | Size | Purpose |
|----------|---------|------|---------|
| `qwen3-asr-1.7b` | qwen3asr | 4.4 GB | **Default** — Realtime + Offline (GPU) |
| `sherpa-onnx-streaming-zipformer-zh-xlarge-int8-2025-06-30` | sherpa-onnx | ~200 MB | Realtime streaming (CPU) |
| `sherpa-onnx-streaming-zipformer-bilingual-zh-en` | sherpa-onnx | ~100 MB | Realtime streaming (CPU, bilingual) |
| `sensevoice-small` | sensevoice | ~80 MB | Offline transcription (CPU/GPU) |

## Model Auto-Download

Models are downloaded on first use if not present. The `ensure_model_dir()` method in each engine class handles HuggingFace downloads:

```python
# engine.py — Qwen3ASROfflineEngine
def ensure_model_dir(self) -> Path:
    model_dir = self._model_dir()
    if model_dir.exists() and (model_dir / "config.json").exists():
        return model_dir
    # Auto-download from HuggingFace
    snapshot_download(
        repo_id="Qwen/Qwen3-ASR-1.7B",
        local_dir=model_dir,
        local_dir_use_symlinks=False,
    )
    return model_dir
```

For production use, pre-download models to avoid first-request timeouts:

```bash
huggingface-cli download Qwen/Qwen3-ASR-1.7B \
  --local-dir /mnt/sdc_old/models/speech/qwen3-asr-1.7b/
```

## Adding a New Speech Model

1. **Add catalog entry** to `managed_model_catalog.json`:
   ```json
   {
     "model_id": "my-new-model",
     "family": "my-model-family",
     "category": "speech",
     "backend": "my-backend",
     "filename": "config.json",
     "relative_dir": "speech/my-new-model",
     "file_path": "/mnt/sdc_old/models/speech/my-new-model/config.json",
     "hf_repo": "org/my-model-repo",
     "source_provider": "huggingface"
   }
   ```

2. **Add engine implementation** in `llm-node/src/speech/`:
   - `engine.py` — Offline engine class
   - `realtime.py` — Realtime engine class + `create_realtime_engine()` dispatch

3. **Add env config** in `app_templates/speech-runtime/env/`

4. **Update Dockerfile** if new pip dependencies are needed

5. **Add `resolve_audio_model_id()` support** if the model ID uses a new prefix

## CrossHare Auto-Start Flow

When CrossHare starts, it attempts to bring up speech services:

```
crosshare main.go
  └── Server.maybeStartSpeechRuntime()     (speech.go:51)
        ├── Check CROSSHARE_SPEECH_AUTOSTART ≠ 0/false
        ├── Spawn goroutine with 45s timeout
        └── localman --speech start --json
              ├── _ensure_app_rendered("speech-runtime")
              │     └── Compare template vs generated → re-render if needed
              ├── _managed_model_files_ready(realtime_model)
              │     └── Check catalog entry file_path exists on disk
              ├── start_app("speech-runtime")
              │     └── bash manage.sh start → docker compose up -d --wait
              └── Wait for health at :8013, :8014
```

If auto-start fails (e.g., model not downloaded, Docker build needed), CrossHare continues running without speech. The UI shows speech status via `speechStatus()`.

## Env File Rendering

The `_ensure_app_rendered()` process copies template files from the source repo to the generated apps directory. It compares file content checkpoints to determine if re-rendering is needed:

```python
# _app_needs_render() checks:
# 1. Does the generated directory exist?
# 2. Do runtime_files hash checkpoints match the inventory?
# 3. Do all runtime_files exist in the generated directory?
# 4. Do file contents match between template and generated?

# If any check fails → full re-render (copy all template files)
# If all pass → skip, preserve generated env
```

This means: **changes to template files trigger re-rendering on next `localman --speech start`**. The generated `speech.env` will be overwritten with the template default. To preserve custom env config, override via the generated directory or use Docker Compose directly.
