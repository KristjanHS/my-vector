// Host test of patch 0010's my-vector voice block (pc-voice.sh cuts it from the patch into pc-voice.inc and builds this).
// Usage: t <tts-urls.txt>   env PC_VOICE_LIVE=<voice> adds POSTs to a live tts/server.py on 127.0.0.1:8091 (never in make check).
#include "json/json.h"
#include <arpa/inet.h>
#include <netinet/in.h>
#include <poll.h>
#include <signal.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>
#include <algorithm>
#include <cctype>
#include <cerrno>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <functional>
#include <string>
#include <vector>
#include "pc-voice.inc"

static int fails = 0, checks = 0;
#define CHECK(c) do { ++checks; if (!(c)) { printf("FAIL %s:%d %s\n", __FILE__, __LINE__, #c); ++fails; } } while (0)

static std::vector<std::string> parts(const std::string& t)
{
  std::vector<std::string> o;
  for (auto& p : MyVectorSplitSentences(t)) { o.push_back(t.substr(p.first, p.second - p.first)); }
  return o;
}

static const std::string kHdr = "HTTP/1.0 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n";

static bool parse(const std::string& r, std::string& why)
{
  std::vector<int16_t> pcm;
  return MyVectorParseResponse(r, pcm, why);
}

static long msSince(std::chrono::steady_clock::time_point t0)
{
  return static_cast<long>(std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - t0).count());
}

// One-connection server on 127.0.0.1 in a child: reads the request, then runs serve(fd). Returns the port; kill(pid) after.
static int serveOnce(pid_t& pid, const std::function<void(int)>& serve)
{
  const int ls = socket(AF_INET, SOCK_STREAM, 0);
  sockaddr_in sa{};
  sa.sin_family = AF_INET;
  sa.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  socklen_t len = sizeof(sa);
  if (ls < 0 || bind(ls, reinterpret_cast<sockaddr*>(&sa), sizeof(sa)) != 0 || listen(ls, 1) != 0 ||
      getsockname(ls, reinterpret_cast<sockaddr*>(&sa), &len) != 0) { perror("serveOnce"); exit(2); }
  pid = fork();
  if (pid == 0) {
    const int fd = accept(ls, nullptr, nullptr);
    std::string req;
    char b[4096];
    ssize_t n;
    while (req.find("}") == std::string::npos && (n = recv(fd, b, sizeof(b), 0)) > 0) { req.append(b, static_cast<size_t>(n)); }
    serve(fd);
    close(fd);
    _exit(0);
  }
  close(ls);
  return ntohs(sa.sin_port);
}

static void reap(pid_t pid)
{
  kill(pid, SIGKILL);
  waitpid(pid, nullptr, 0);
}

static void sendAll(int fd, const std::string& s)
{
  for (size_t off = 0; off < s.size();) {
    const ssize_t n = send(fd, s.data() + off, s.size() - off, MSG_NOSIGNAL);
    if (n <= 0) { return; }
    off += static_cast<size_t>(n);
  }
}

int main(int argc, char** argv)
{
  if (argc != 2) { fprintf(stderr, "usage: %s <tts-urls.txt>\n", argv[0]); return 2; }
  signal(SIGPIPE, SIG_IGN);
  std::string why;

  // splitter + plain text: as TextToSpeechComponent hands lines over (trailing \pau=10\ + punctuation)
  auto p = parts("Hi there. How are you \\pau=10\\?");
  CHECK(p.size() == 2 && p[0] == "Hi there." && p[1] == " How are you \\pau=10\\?");
  CHECK(MyVectorPlainText(p[1]) == "How are you?");
  CHECK(parts("Pi is 3.14 ok \\pau=10\\.").size() == 1);
  CHECK(parts("Wow!! Really?! Yes").size() == 3);
  CHECK(parts("a \\rspd=1.5\\ b. c").size() == 2);
  CHECK(parts("Done.   ").size() == 1);
  CHECK(MyVectorPlainText(" \\pau=10\\ ").empty());

  // response parser: happy paths
  {
    std::vector<int16_t> pcm;
    const std::string ok = kHdr + std::string("4\r\n\x01\x00\x02\x00\r\n2\r\n\x03\x00\r\n0\r\n\r\n", 20);
    CHECK(MyVectorParseResponse(ok, pcm, why) && pcm.size() == 3 && pcm[0] == 1 && pcm[2] == 3);
    CHECK(!parse(ok.substr(0, ok.size() - 5), why) && why == "stream cut");
    CHECK(!parse("HTTP/1.0 400 Bad Request\r\n\r\n{}", why));
    CHECK(MyVectorParseResponse(std::string("HTTP/1.0 200 OK\r\n\r\n\x05\x00", 21), pcm, why) && pcm.size() == 1 && pcm[0] == 5);
    CHECK(parse(kHdr + std::string("2;ext=1\r\n\x07\x00\r\n0\r\n\r\n", 17), why)); // chunk extension
  }
  // chunk-size line: hostile sizes must fail fast (old sum check wrapped on 32-bit size_t -> endless loop, bad_alloc)
  {
    char wrap[32]; // lineEnd + 2 + n + 2 wraps back to this very line on this host's size_t, as fffffff4 does on vic-anim's 32-bit one
    snprintf(wrap, sizeof(wrap), "%zx", SIZE_MAX - 2 * sizeof(size_t) - 3);
    CHECK(!parse(kHdr + wrap + "\r\nabcd\r\n0\r\n\r\n", why) && why == "bad chunk size");
    CHECK(!parse(kHdr + "fffffff4\r\nabcd\r\n0\r\n\r\n", why) && why == "bad chunk size");
    CHECK(!parse(kHdr + "-1\r\nabcd\r\n0\r\n\r\n", why) && why == "bad chunk size");
    CHECK(!parse(kHdr + " 2\r\nab\r\n0\r\n\r\n", why) && why == "bad chunk size");
    CHECK(!parse(kHdr + "2x\r\nab\r\n0\r\n\r\n", why) && why == "bad chunk size");
    CHECK(!parse(kHdr + "\r\nab\r\n0\r\n\r\n", why) && why == "bad chunk size");
    char big[32];
    snprintf(big, sizeof(big), "%zx", kMyVectorMaxResponse + 1);
    CHECK(!parse(kHdr + big + "\r\nab\r\n0\r\n\r\n", why) && why == "bad chunk size");
    snprintf(big, sizeof(big), "%zx", kMyVectorMaxResponse);
    CHECK(!parse(kHdr + big + "\r\nab\r\n0\r\n\r\n", why) && why == "stream cut");
    CHECK(!parse(kHdr + "2\r\nab", why) && why == "stream cut"); // data but no trailing CRLF
  }
  CHECK(kMyVectorMaxResponse >= 120u * kMyVectorPcRate * 2 + 4096); // 2 min of s16 + framing

  // resampler
  std::vector<int16_t> in(16000, 100);
  CHECK(MyVectorResample(in, 16000, 22050).size() == 22050);

  // voice file: fixed cases, then every tts-urls.txt row (voice-push.sh runs the same rows in pc-voice.sh)
  {
    const auto load = [&](const char* json, std::string& h, int& po, std::string& pa, std::string& vo, float& sl) {
      if (json) { FILE* f = fopen(MV_TEST_VOICE_FILE, "w"); fputs(json, f); fclose(f); } else { remove(MV_TEST_VOICE_FILE); }
      return MyVectorLoadVoice(h, po, pa, vo, sl, why);
    };
    std::string h, pa, vo;
    int po = 0;
    float sl = 0;
    CHECK(!load(nullptr, h, po, pa, vo, sl) && why.find("no ") == 0);
    CHECK(!load("{nope", h, po, pa, vo, sl) && why == "unparseable voice file");
    CHECK(!load("{\"url\":\"http://1.2.3.4:8091\",\"voice\":\"stock\"}", h, po, pa, vo, sl) && why == "voice stock");
    CHECK(!load("{\"url\":\"\",\"voice\":\"cori\"}", h, po, pa, vo, sl) && why == "no url");
    CHECK(load("{\"url\":\"http://192.168.1.5:8091/\",\"voice\":\"cori\",\"slow\":1.3}", h, po, pa, vo, sl));
    CHECK(h == "192.168.1.5" && po == 8091 && pa.empty() && vo == "cori" && sl > 1.29f && sl < 1.31f);
    CHECK(load("{\"url\":\"http://10.0.0.2/x/\",\"voice\":\"af_bella\"}", h, po, pa, vo, sl));
    CHECK(h == "10.0.0.2" && po == 80 && pa == "/x" && sl == 1.f);
    std::ifstream urls(argv[1]);
    std::string want, url;
    int rows = 0;
    while (urls >> want) {
      if (want[0] == '#') { std::getline(urls, url); continue; }
      urls >> url;
      Json::Value v;
      v["url"] = url;
      v["voice"] = "af_bella";
      const bool got = load(Json::FastWriter().write(v).c_str(), h, po, pa, vo, sl);
      if (got != (want == "ok")) { printf("FAIL tts-urls.txt: firmware %s %s (%s)\n", got ? "accepts" : "rejects", url.c_str(), got ? "" : why.c_str()); }
      CHECK(got == (want == "ok"));
      ++rows;
    }
    CHECK(rows >= 8);
    remove(MV_TEST_VOICE_FILE);
  }

  // POST against a local server (pc-voice.sh builds with kMyVectorSentence_ms = 600)
  {
    pid_t pid;
    std::string resp;
    std::vector<int16_t> pcm;
    int port = serveOnce(pid, [](int fd) { sendAll(fd, kHdr + std::string("2\r\n\x09\x00\r\n0\r\n\r\n", 12)); });
    CHECK(MyVectorPost("127.0.0.1", port, "", "{}", resp, why) && MyVectorParseResponse(resp, pcm, why) && pcm.size() == 1 && pcm[0] == 9);
    reap(pid);

    // silent after the header: the sentence deadline ends it, not the 5 s idle timeout
    port = serveOnce(pid, [](int fd) { sendAll(fd, kHdr); sleep(8); });
    auto t0 = std::chrono::steady_clock::now();
    CHECK(!MyVectorPost("127.0.0.1", port, "", "{}", resp, why) && why == "sentence over 20 s");
    long ms = msSince(t0);
    CHECK(ms >= kMyVectorSentence_ms - 50 && ms < kMyVectorSentence_ms + 700);
    reap(pid);

    // data still queued past the deadline: stops at the first read after it, not when the server pauses
    port = serveOnce(pid, [](int fd) { usleep(900 * 1000); sendAll(fd, kHdr + std::string(256 * 1024, 'x')); sleep(8); });
    CHECK(!MyVectorPost("127.0.0.1", port, "", "{}", resp, why) && why == "sentence over 20 s" && resp.size() <= 8192);
    reap(pid);
  }

  if (const char* live = getenv("PC_VOICE_LIVE")) {
    Json::Value req;
    req["text"] = "Hello there, I am Vector.";
    req["voice"] = live;
    req["slow"] = 1.0;
    std::string resp;
    std::vector<int16_t> pcm;
    auto t0 = std::chrono::steady_clock::now();
    bool okp = MyVectorPost("127.0.0.1", 8091, "", Json::FastWriter().write(req), resp, why) && MyVectorParseResponse(resp, pcm, why);
    printf("live %s: ok=%d why=%s samples=%zu (%.2fs audio) in %ld ms\n", live, okp, why.c_str(), pcm.size(), pcm.size() / 16000.0, msSince(t0));
    CHECK(okp && pcm.size() > 8000);
    CHECK(!MyVectorPost("127.0.0.1", 1, "", "{}", resp, why));
    printf("closed port: %s\n", why.c_str());
    t0 = std::chrono::steady_clock::now();
    CHECK(!MyVectorPost("10.255.255.1", 8091, "", "{}", resp, why));
    const long ms = msSince(t0);
    printf("blackhole: %s in %ld ms\n", why.c_str(), ms);
    CHECK(ms >= 1400 && ms < 1700);
    req["voice"] = "stock";
    okp = MyVectorPost("127.0.0.1", 8091, "", Json::FastWriter().write(req), resp, why) && MyVectorParseResponse(resp, pcm, why);
    printf("bad voice: %s\n", why.c_str());
    CHECK(!okp);
  }
  printf("%s: %d checks, %d failures\n", fails ? "RED" : "GREEN", checks, fails);
  return fails != 0;
}
