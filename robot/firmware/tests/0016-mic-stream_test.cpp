// Host test of patch 0016's my-vector mic-stream blocks (mic-stream.sh cuts them from the patch into mic-stream.inc and builds this).
// Pure part: ring, gate, tap, URL host. Sender: stubbed logging/console var, a real loopback TCP listener.
#include "json/json.h"
#include <arpa/inet.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <pthread.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <unistd.h>
#include <algorithm>
#include <array>
#include <atomic>
#include <cerrno>
#include <chrono>
#include <condition_variable>
#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

#define ANKI_PLATFORM_VICOS 1 // compiles the affinity block too (a failed pin only logs)
#define CONSOLE_VAR_RANGED(type, name, path, def, lo, hi) type name = def
#define MYVECTOR_MIC_HEALTHY_S 1 // a healthy connection in 1 s, not 10
static std::mutex mvLogMu;
static std::vector<std::string> mvLog;
__attribute__((format(printf, 2, 3))) static void MvLog(const char* ev, const char* fmt, ...)
{
  char b[512];
  va_list ap;
  va_start(ap, fmt);
  vsnprintf(b, sizeof(b), fmt, ap);
  va_end(ap);
  std::lock_guard<std::mutex> l(mvLogMu);
  mvLog.push_back(std::string(ev) + " " + b);
}
#define PRINT_NAMED_WARNING MvLog
static int mvLogged(const char* needle)
{
  std::lock_guard<std::mutex> l(mvLogMu);
  return static_cast<int>(std::count_if(mvLog.begin(), mvLog.end(), [&](const std::string& s) { return s.find(needle) != std::string::npos; }));
}
// The blocks' pthread_create, failing on demand (EAGAIN = the thread limit).
static int mvCreateFail = 0, mvCreates = 0;
static int mvPthreadCreate(pthread_t* t, const pthread_attr_t* a, void* (*f)(void*), void* arg)
{
  ++mvCreates;
  return mvCreateFail != 0 ? mvCreateFail : pthread_create(t, a, f, arg);
}
#define pthread_create mvPthreadCreate
namespace Anki {
namespace Util { inline void SetThreadName(pthread_t, const char*) {} }
namespace AudioUtil { using AudioSample = int16_t; }
namespace Vector {
namespace MicData {
constexpr uint32_t kSamplesPerBlockPerChannel = 160;
#include "mic-stream.inc"
} // namespace MicData
} // namespace Vector
} // namespace Anki
using namespace Anki::Vector::MicData;

static int fails = 0, checks = 0;
#define CHECK(c) do { ++checks; if (!(c)) { printf("FAIL %s:%d %s\n", __FILE__, __LINE__, #c); ++fails; } } while (0)

// Block <seq>: [0] = low 16 bits, [1] = high 16 bits, [i>=2] = low + i (a torn copy breaks the pattern).
struct Blk { int16_t s[kMyVectorMicBlock]; };
static Blk mk(uint32_t seq)
{
  Blk b;
  b.s[0] = static_cast<int16_t>(seq & 0xffff);
  b.s[1] = static_cast<int16_t>(seq >> 16);
  for (uint32_t i = 2; i < kMyVectorMicBlock; ++i) { b.s[i] = static_cast<int16_t>((seq & 0xffff) + i); }
  return b;
}
// seq of a block, or -1 if torn.
static long seqOf(const int16_t* s)
{
  const uint32_t lo = static_cast<uint16_t>(s[0]);
  for (uint32_t i = 2; i < kMyVectorMicBlock; ++i) {
    if (s[i] != static_cast<int16_t>(lo + i)) { return -1; }
  }
  return static_cast<long>(lo | (static_cast<uint32_t>(static_cast<uint16_t>(s[1])) << 16));
}

static std::vector<int16_t> buf(kMyVectorMicRingBlocks * kMyVectorMicBlock);
// Drains the ring: the seqs read, in order.
static std::vector<long> drain(MyVectorMicRing& r)
{
  std::vector<long> o;
  const uint32_t n = r.Read(buf.data(), kMyVectorMicRingBlocks);
  for (uint32_t i = 0; i < n; ++i) { o.push_back(seqOf(buf.data() + i * kMyVectorMicBlock)); }
  return o;
}
static std::vector<long> range(long a, long b)
{
  std::vector<long> o;
  for (long i = a; i < b; ++i) { o.push_back(i); }
  return o;
}

// Feeds blocks seq [from, to) with one vad value through the tap at <port>.
static void feed(MyVectorMicTap& t, int port, uint32_t from, uint32_t to, bool vad)
{
  for (uint32_t i = from; i < to; ++i) { const Blk b = mk(i); t.Block(port, b.s, vad); }
}

static void testGate()
{
  auto* t = new MyVectorMicTap;
  feed(*t, 8093, 0, 500, false); // gate closed: silence sends nothing
  CHECK(drain(t->ring).empty());
  // 500 silent, 50 speech, 200 silent: pre-roll 470..499, speech 500..549, hangover 550..589.
  feed(*t, 8093, 500, 550, true);
  feed(*t, 8093, 550, 750, false);
  CHECK(drain(t->ring) == range(470, 590));
  // 170 silent, 20 speech, 30 silent (< hangover: re-armed), 5 speech, 100 silent: one run from pre-roll 730.
  feed(*t, 8093, 750, 760, false);
  CHECK(drain(t->ring).empty());
  feed(*t, 8093, 760, 780, true);
  feed(*t, 8093, 780, 810, false);
  feed(*t, 8093, 810, 815, true);
  feed(*t, 8093, 815, 915, false);
  CHECK(drain(t->ring) == range(730, 855));
  delete t;
}

static void testRingOverflow()
{
  auto* r = new MyVectorMicRing;
  for (uint32_t i = 0; i < 250; ++i) { const Blk b = mk(i); r->Push(b.s); }
  CHECK(drain(*r) == range(50, 250)); // drop oldest
  CHECK(r->Drops() == 50);
  for (uint32_t i = 250; i < 260; ++i) { const Blk b = mk(i); r->Push(b.s); }
  CHECK(drain(*r) == range(250, 260));
  CHECK(r->Drops() == 50);
  delete r;
}

// C3 under a real race: a free-running writer laps a slow reader; every block out is whole, in order, and out + drops = in.
static void testRingRace()
{
  auto* r = new MyVectorMicRing;
  const uint32_t total = 2000000;
  std::atomic<bool> done{false};
  std::thread w([&] {
    for (uint32_t i = 0; i < total; ++i) { const Blk b = mk(i); r->Push(b.s); }
    done = true;
  });
  std::vector<int16_t> out(kMyVectorMicRingBlocks * kMyVectorMicBlock);
  long last = -1, torn = 0, disorder = 0, got = 0;
  bool fin = false;
  while (!fin) {
    fin = done.load();
    const uint32_t n = r->Read(out.data(), kMyVectorMicRingBlocks);
    for (uint32_t i = 0; i < n; ++i) {
      const long s = seqOf(out.data() + i * kMyVectorMicBlock);
      if (s < 0) { ++torn; } else if (s <= last) { ++disorder; } else { last = s; }
    }
    got += n;
  }
  w.join();
  CHECK(torn == 0);
  CHECK(disorder == 0);
  CHECK(got + static_cast<long>(r->Drops()) == total);
  CHECK(r->Drops() > 0); // vacuity: the writer did lap the reader
  printf("ring race: %ld blocks read, %llu dropped\n", got, static_cast<unsigned long long>(r->Drops()));
  delete r;
}

static void testPortZero()
{
  auto* t = new MyVectorMicTap;
  bool turned = false;
  for (uint32_t i = 0; i < 100; ++i) { const Blk b = mk(i); turned = t->Block(0, b.s, true) || turned; }
  CHECK(!turned && !t->on);
  CHECK(drain(t->ring).empty());
  const Blk b = mk(100);
  CHECK(t->Block(8093, b.s, false)); // the block that turns it on
  feed(*t, 8093, 101, 110, false);
  feed(*t, 8093, 110, 111, true);
  CHECK(drain(t->ring) == range(100, 111)); // pre-roll holds no port-0 block
  feed(*t, 8093, 111, 120, true);
  feed(*t, 0, 120, 130, true); // off mid-speech: gate reset
  CHECK(drain(t->ring) == range(111, 120));
  feed(*t, 8093, 130, 135, false);
  CHECK(drain(t->ring).empty()); // no hangover carried across port 0
  delete t;
}

static void testHost()
{
  std::string h;
  CHECK(MyVectorMicStreamHost("http://192.0.2.249:8091", h) && h == "192.0.2.249");
  CHECK(MyVectorMicStreamHost("http://10.0.0.1", h) && h == "10.0.0.1");
  CHECK(MyVectorMicStreamHost("http://10.0.0.1/tts", h) && h == "10.0.0.1");
  CHECK(!MyVectorMicStreamHost("https://10.0.0.1:8091", h));
  CHECK(!MyVectorMicStreamHost("http://pc.local:8091", h));
  CHECK(!MyVectorMicStreamHost("http://10.0.1:8091", h));
  CHECK(!MyVectorMicStreamHost("http://", h));
}

static long msSince(std::chrono::steady_clock::time_point t0)
{
  return static_cast<long>(std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - t0).count());
}
// Accepts within ms; -1 on timeout.
static int acceptIn(int ls, int ms)
{
  pollfd p{ls, POLLIN, 0};
  return poll(&p, 1, ms) == 1 ? accept(ls, nullptr, nullptr) : -1;
}
// Reads until want bytes, EOF or ms of quiet; returns the bytes; eof set on a clean close.
static std::string readFor(int fd, size_t want, int ms, bool& eof)
{
  std::string s;
  char b[8192];
  eof = false;
  pollfd p{fd, POLLIN, 0};
  while (s.size() < want && poll(&p, 1, ms) == 1) {
    const ssize_t n = recv(fd, b, sizeof(b), 0);
    if (n <= 0) { eof = n == 0; break; }
    s.append(b, static_cast<size_t>(n));
  }
  return s;
}
// Runs ProcessTriggerLoop's hook (cut from the patch) on block <seq>.
static void hook(uint32_t seq, bool vad)
{
  const Blk b = mk(seq);
  std::array<int16_t, kMyVectorMicBlock> processedAudio;
  std::copy(b.s, b.s + kMyVectorMicBlock, processedAudio.begin());
  struct { int myVectorVad; } slot{vad ? 1 : 0};
  const auto* readyDataSpot = &slot;
#include "mic-stream-hook.inc"
}

// The sender's fd behind accepted connection c (same process): the socket whose local address is c's peer.
static int senderFd(int c)
{
  sockaddr_in peer{}, me{};
  socklen_t l = sizeof(peer);
  if (getpeername(c, reinterpret_cast<sockaddr*>(&peer), &l) != 0) { return -1; }
  for (int fd = 0; fd < 1024; ++fd) {
    l = sizeof(me);
    if (fd != c && getsockname(fd, reinterpret_cast<sockaddr*>(&me), &l) == 0 && l == sizeof(me) &&
        me.sin_port == peer.sin_port && me.sin_addr.s_addr == peer.sin_addr.s_addr) { return fd; }
  }
  return -1;
}
static int sockOpt(int fd, int level, int name)
{
  int v = -1;
  socklen_t l = sizeof(v);
  return getsockopt(fd, level, name, &v, &l) == 0 ? v : -1;
}

// No sender thread (pthread_create fails): one log line, no retry on a later off -> on, the stream stays off.
static void testSenderStartFails()
{
  mvCreateFail = EAGAIN;
  __atomic_store_n(&kMyVector_MicStreamPort, 1, __ATOMIC_RELAXED);
  for (uint32_t i = 0; i < 50; ++i) { hook(i, true); }
  __atomic_store_n(&kMyVector_MicStreamPort, 0, __ATOMIC_RELAXED);
  hook(50, false);
  __atomic_store_n(&kMyVector_MicStreamPort, 1, __ATOMIC_RELAXED);
  hook(51, true);
  CHECK(mvCreates == 1 && !sMyVectorMicSenderRunning);
  CHECK(mvLogged("no sender thread") == 1);
  __atomic_store_n(&kMyVector_MicStreamPort, 0, __ATOMIC_RELAXED);
  hook(52, false);
  sMyVectorMicTap.ring.Skip(); // no reader ran: drop these blocks before testSender's sender starts
  mvCreateFail = 0;
  mvCreates = 0;
  sMyVectorMicSenderTried = false;
}

static void testSender()
{
  const int ls = socket(AF_INET, SOCK_STREAM, 0);
  const int rcvbuf = 4096; // accepted sockets inherit it: a peer that never reads fills up fast (stall)
  setsockopt(ls, SOL_SOCKET, SO_RCVBUF, &rcvbuf, sizeof(rcvbuf));
  sockaddr_in sa{};
  sa.sin_family = AF_INET;
  sa.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  socklen_t len = sizeof(sa);
  if (ls < 0 || bind(ls, reinterpret_cast<sockaddr*>(&sa), sizeof(sa)) != 0 || listen(ls, 4) != 0 ||
      getsockname(ls, reinterpret_cast<sockaddr*>(&sa), &len) != 0) { perror("listen"); exit(2); }
  const int port = ntohs(sa.sin_port);
  std::ofstream(MV_TEST_VOICE_FILE) << "{\"url\":\"http://127.0.0.1:8091\",\"voice\":\"af_bella\"}";

  for (uint32_t i = 0; i < 100; ++i) { hook(i, true); } // port 0: no thread, no socket
  CHECK(!sMyVectorMicSenderRunning && mvCreates == 0);
  CHECK(acceptIn(ls, 300) < 0);

  __atomic_store_n(&kMyVector_MicStreamPort, port, __ATOMIC_RELAXED);
  for (uint32_t i = 100; i < 130; ++i) { hook(i, false); }
  for (uint32_t i = 130; i < 150; ++i) { hook(i, true); }
  for (uint32_t i = 150; i < 300; ++i) { hook(i, false); }
  CHECK(sMyVectorMicSenderRunning);
  int c = acceptIn(ls, 2000);
  CHECK(c >= 0);
  bool eof;
  const std::string got = readFor(c, 1u << 20, 600, eof);
  const int sfd = senderFd(c); // read after the data: the sender set its options right after connect
  CHECK(sockOpt(sfd, SOL_SOCKET, SO_KEEPALIVE) == 1 && sockOpt(sfd, IPPROTO_TCP, TCP_KEEPIDLE) == 5 &&
        sockOpt(sfd, IPPROTO_TCP, TCP_KEEPINTVL) == 2 && sockOpt(sfd, IPPROTO_TCP, TCP_KEEPCNT) == 3 &&
        sockOpt(sfd, IPPROTO_TCP, TCP_USER_TIMEOUT) == 3000);
  std::vector<long> seqs;
  for (size_t o = 0; o + sizeof(Blk) <= got.size(); o += sizeof(Blk)) { seqs.push_back(seqOf(reinterpret_cast<const int16_t*>(got.data() + o))); }
  CHECK(got.size() % sizeof(Blk) == 0);
  CHECK(seqs == range(100, 190)); // pre-roll 30 + speech 20 + hangover 40, raw s16, nothing else

  // The PC closes: the sender sees it while idle and redials after the >= 1 s backoff.
  close(c);
  auto t0 = std::chrono::steady_clock::now();
  c = acceptIn(ls, 4000);
  const long redial = msSince(t0);
  CHECK(c >= 0 && redial >= 900);
  // Accept-then-close: a short-lived connection does not reset the backoff, so the next gap doubles.
  close(c);
  t0 = std::chrono::steady_clock::now();
  c = acceptIn(ls, 5000);
  const long redial2 = msSince(t0);
  CHECK(c >= 0 && redial2 >= 1900);
  // A connection that lived kMyVectorMicHealthyS resets the backoff (4 s by now) to the 1 s redial.
  std::this_thread::sleep_for(std::chrono::milliseconds(1300));
  close(c);
  t0 = std::chrono::steady_clock::now();
  c = acceptIn(ls, 6000);
  const long redial3 = msSince(t0);
  CHECK(c >= 0 && redial3 < 1500);
  printf("sender: %zu bytes, redials after %ld, %ld, then healthy %ld ms\n", got.size(), redial, redial2, redial3);

  // A short port 0 (inside one 100 ms wait) still forces a fresh connection, at once (backoff reset).
  __atomic_store_n(&kMyVector_MicStreamPort, 0, __ATOMIC_RELAXED);
  hook(300, false);
  __atomic_store_n(&kMyVector_MicStreamPort, port, __ATOMIC_RELAXED);
  hook(301, false);
  readFor(c, 1, 1000, eof);
  CHECK(eof);
  close(c);
  t0 = std::chrono::steady_clock::now();
  c = acceptIn(ls, 1000);
  CHECK(c >= 0 && msSince(t0) < 500);

  // C4 stall: the PC accepts but never reads; once the buffers fill, pending stops shrinking: close + redial.
  t0 = std::chrono::steady_clock::now();
  int c2 = -1;
  for (uint32_t seq = 1000; c2 < 0 && msSince(t0) < 20000;) {
    for (int i = 0; i < 50; ++i) { hook(seq++, true); }
    c2 = acceptIn(ls, 10);
  }
  CHECK(c2 >= 0 && mvLogged("closed: send stalled") == 1);
  printf("stall: redial after %ld ms\n", msSince(t0));
  close(c);
  c = c2;

  __atomic_store_n(&kMyVector_MicStreamPort, 0, __ATOMIC_RELAXED); // port 0: the sender closes the socket
  readFor(c, 1u << 24, 2500, eof); // drains what the stall run left queued
  CHECK(eof);
  close(c);
  t0 = std::chrono::steady_clock::now();
  MyVectorMicStreamStop();
  CHECK(!sMyVectorMicSenderRunning && msSince(t0) < 500);
  close(ls);
}

int main()
{
  (void)sMyVectorRawVad; // set by ProcessMicrophonesSE, outside the cut blocks
  testGate();
  testRingOverflow();
  testRingRace();
  testPortZero();
  testHost();
  testSenderStartFails();
  testSender();
  printf("%s: %d checks, %d failures\n", fails ? "RED" : "GREEN", checks, fails);
  if (fails) {
    std::lock_guard<std::mutex> l(mvLogMu);
    for (auto& s : mvLog) { printf("log: %s\n", s.c_str()); }
  }
  return fails != 0;
}
