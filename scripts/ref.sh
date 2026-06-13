#!/usr/bin/env bash
# Render an example's manim reference scene (ref.py) and drop the resulting
# video at <dir>/ref/result.mp4, mirroring our own <dir>/res/result.mp4.
#
# Python is driven through `uv` so no global manim install is required:
# `uv run --with manim` materialises manim in an ephemeral environment.
# Best-effort: if uv is missing or manim can't render (e.g. a TeX scene with no
# LaTeX installed) we warn and exit 0 so `make` stays green.
set -euo pipefail

dir="${1:?usage: ref.sh <example-dir>}"
quality="${MANIM_QUALITY:-l}" # l / m / h / k
py="$dir/ref.py"
out="$dir/ref"

if [ ! -f "$py" ]; then
	echo "ref: no $py; skipping"
	exit 0
fi
if ! command -v uv >/dev/null 2>&1; then
	echo "ref: uv not found; skipping reference render for $dir"
	exit 0
fi

rm -rf "$out"
mkdir -p "$out"

if ! uv run --with manim manim render -q"$quality" -a --media_dir "$out" "$py"; then
	echo "ref: manim render failed for $py (missing system deps such as LaTeX?); skipping"
	exit 0
fi

mp4="$(find "$out/videos" -name '*.mp4' -not -path '*/partial_movie_files/*' 2>/dev/null | head -n1 || true)"
if [ -n "$mp4" ]; then
	# manim leaves the moov atom at the end of the file; web/preview players
	# then can't find the codec config up front and report a bogus "missing
	# H.264 decoder". Remux (no re-encode) to move moov to the front so the
	# refs play like our own res/result.mp4. Fall back to a plain copy if
	# ffmpeg isn't available.
	if command -v ffmpeg >/dev/null 2>&1 &&
		ffmpeg -nostdin -v error -y -i "$mp4" -c copy -movflags +faststart "$out/result.mp4"; then
		echo "ref: $out/result.mp4 (faststart)"
	else
		cp "$mp4" "$out/result.mp4"
		echo "ref: $out/result.mp4 (plain copy; ffmpeg faststart unavailable)"
	fi
else
	echo "ref: no mp4 produced for $py"
fi
