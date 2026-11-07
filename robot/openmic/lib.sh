# shellcheck shell=bash
# Open-mic pieces probe.sh and listen.sh source: robot facts (env.sh), service URLs, silent RecordMicDataClip clips → 16 kHz pcm.
lib_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=robot/env.sh
. "$lib_dir/../env.sh"
# shellcheck disable=SC2034 # read by the sourcing scripts
tts=${TTS_URL:-http://127.0.0.1:8091} stt=${STT_URL:-http://127.0.0.1:8092} api=${WIREPOD_URL:-http://127.0.0.1:8080}
ffmpeg=$(command -v ffmpeg || echo /opt/ffmpeg/ffmpeg)
D=/data/data/com.anki.victor/cache/micdata/debugCapture
ssh_opts+=(-o ServerAliveInterval=5 -o ServerAliveCountMax=2)
rsh() { ssh "${ssh_opts[@]}" "root@$ip" "$@"; }
clip_ms() { curl -fsS --max-time 15 -o /dev/null "http://$ip:8889/consolevarset?key=MicData_ClipRecordTime_ms&value=$1"; }
record_clip() { curl -fsS --max-time 15 -o /dev/null "http://$ip:8889/consolefunccall?func=RecordMicDataClip&args="; }
# saved_clip: 0 once the clip's two wavs stop growing (10 s max), else prints the last "<files> <bytes>" and returns 1.
saved_clip() {
  local a b
  for _ in {1..10}; do
    a=$(rsh "ls -l $D/miccapture_*/*.wav 2>/dev/null | awk '{s+=\$5} END {print NR, s}'"); sleep 1
    b=$(rsh "ls -l $D/miccapture_*/*.wav 2>/dev/null | awk '{s+=\$5} END {print NR, s}'")
    [[ $a == "$b" && $a == 2\ * ]] && return 0
  done
  echo "$b"
  return 1
}
# park_clip: saved_clip's rule in one robot-side POSIX sh loop, then mv $D $D.prev; else prints the last "<files> <bytes>" and returns 1.
park_clip() {
  rsh "D=$D; "'w() { ls -l $D/miccapture_*/*.wav 2>/dev/null | awk "{s+=\$5} END {print NR, s}"; }; n=0; b=
while [ $n -lt 10 ]; do a=$(w); sleep 1; b=$(w); case $a in "2 "*) [ "$a" = "$b" ] && { mv $D $D.prev; exit; } ;; esac; n=$((n + 1)); done
echo "$b"; exit 1'
}
fetch_clip() { mkdir -p "$1" && scp -q "${ssh_opts[@]}" "root@$ip:$D/miccapture_*/*.wav" "$1/" && rsh "rm -rf $D"; }
proc_pcm() { "$ffmpeg" -v error -y -i "$(find "$1" -name '*.wav' ! -name '*_raw.wav')" -ar 16000 -ac 1 -f s16le "$1/proc.pcm"; }
raw_pcm() {
  "$ffmpeg" -v error -y -i "$(find "$1" -name '*_raw.wav')" -af 'pan=mono|c0=0.25*c0+0.25*c1+0.25*c2+0.25*c3,dynaudnorm' -ar 16000 -f s16le "$1/raw.pcm"
}
