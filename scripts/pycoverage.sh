#!/bin/bash
# Coverage of the PyTorch side of the AI. Target: 100% of pytorch/*.py.
set -u
cd "$(dirname "$0")/.." || exit 1
.venv/bin/python -m pytest -q pytorch/tests --cov=pytorch --cov-report=term-missing "$@"
