// tests/test_common.cpp - checks for the portable parts of src/common.h (no Windows needed).
//   g++ -std=c++17 -Wall -Wextra -Isrc tests/test_common.cpp -o /tmp/hom_test && /tmp/hom_test
#include "common.h"
#include <cstdio>

static int g_fail = 0;
#define CHECK(x) do { if (!(x)) { std::printf("FAIL line %d: %s\n", __LINE__, #x); ++g_fail; } } while (0)

using namespace homcommon;

static void TestNames() {
    std::string why;
    CHECK(IsSafeName("input-overlay", &why, true));
    CHECK(IsSafeName("clip.v2_final", &why, true));
    CHECK(!IsSafeName("", &why, true));
    CHECK(!IsSafeName(".", &why, true));
    CHECK(!IsSafeName(".installed", &why, true));
    CHECK(IsSafeName(".gitignore", &why, false));
    CHECK(!IsSafeName("a..b", &why, false));
    CHECK(!IsSafeName("a/b", &why, false));
    CHECK(!IsSafeName("a\\b", &why, false));
    CHECK(!IsSafeName("C:x", &why, false));
    CHECK(!IsSafeName("a|b", &why, false));
    CHECK(!IsSafeName("name.", &why, false));
    CHECK(!IsSafeName("name ", &why, false));
    CHECK(!IsSafeName("nul", &why, true));
    CHECK(!IsSafeName("NUL.hrp", &why, false));
    CHECK(!IsSafeName("com1", &why, true));
    CHECK(!IsSafeName("Lpt9.txt", &why, false));
    CHECK(IsSafeName("com10", &why, true));
    CHECK(IsSafeName("console", &why, true));
    CHECK(!IsSafeName(std::string(101, 'a'), &why, true));
}

static void TestVersion() {
    CHECK(VersionGt("1.2.10", "1.2.9"));
    CHECK(!VersionGt("1.2.9", "1.2.10"));
    CHECK(!VersionGt("1.0", "1.0.0"));
    CHECK(VersionGt("1.0.1", "1.0"));
    CHECK(VersionGt("v2.0", "1.9"));
    CHECK(!VersionGt("", "1.0"));
    CHECK(VersionGt("0.6.2", "0.6.1"));
    CHECK(!VersionGt("0.6.1", "0.6.1"));
}

static void TestJson() {
    JsonVal v;
    CHECK(ParseJson("{\"a\": \"x\\\"y\", \"n\": 12, \"b\": true, \"z\": null, \"o\": {\"k\": [1, 2, {\"q\": \"}]\"}]}}", &v));
    CHECK(v.Text("a") == "x\"y");
    CHECK(v.Text("n") == "12");
    CHECK(v.Text("b") == "true");
    CHECK(v.Text("z").empty());
    CHECK(v.Text("missing").empty());
    const JsonVal *o = v.Get("o");
    CHECK(o && o->kind == JsonVal::Obj);
    const JsonVal *k = o ? o->Get("k") : nullptr;
    CHECK(k && k->kind == JsonVal::Arr && k->items.size() == 3);
    CHECK(k && k->items[2].Text("q") == "}]");

    // the value "file" must not be mistaken for the key "file"
    CHECK(ParseJson("{\"type\": \"file\", \"file\": \"intro.mp4\"}", &v));
    CHECK(v.Text("file") == "intro.mp4" && v.Text("type") == "file");
    CHECK(ParseJson("{\"name\": \"type\", \"type\": \"archive\"}", &v));
    CHECK(v.Text("type") == "archive");

    // escapes and unicode, including a surrogate pair
    CHECK(ParseJson("{\"s\": \"\\u0041\\u00e9\\u20ac\\ud83d\\ude00\\n\"}", &v));
    CHECK(v.Text("s") == "A\xC3\xA9\xE2\x82\xAC\xF0\x9F\x98\x80\n");

    // BOM, whitespace, empty containers
    CHECK(ParseJson("\xEF\xBB\xBF  { \"p\" : [ ] , \"q\" : { } }  ", &v));
    CHECK(v.Get("p") && v.Get("p")->items.empty());

    // broken input fails instead of returning half of it
    CHECK(!ParseJson("{\"a\": \"unterminated", &v));
    CHECK(!ParseJson("{\"a\" 1}", &v));
    CHECK(!ParseJson("{\"a\": [1, 2}", &v));
    CHECK(!ParseJson("", &v));
    CHECK(!ParseJson("{\"a\": \"\\x\"}", &v));
    CHECK(!ParseJson(std::string(200, '[') + std::string(200, ']'), &v)); // depth limit
}

static void TestUrls() {
    std::string out, why;
    CHECK(NormalizeRepoUrl("someone/hom", &out, &why) && out == "https://raw.githubusercontent.com/someone/hom/main/pkgs/");
    CHECK(NormalizeRepoUrl("someone/hom@dev", &out, &why) && out == "https://raw.githubusercontent.com/someone/hom/dev/pkgs/");
    CHECK(NormalizeRepoUrl("https://example.org/p", &out, &why) && out == "https://example.org/p/");
    CHECK(!NormalizeRepoUrl("http://example.org/p/", &out, &why));
    CHECK(!NormalizeRepoUrl("ftp://example.org/p/", &out, &why));
    CHECK(!NormalizeRepoUrl("https://user@example.org/p/", &out, &why));
    CHECK(!NormalizeRepoUrl("https://example.org/../x/", &out, &why));
    CHECK(!NormalizeRepoUrl("https://exa mple.org/", &out, &why));
    CHECK(!NormalizeRepoUrl("a/b/c", &out, &why));
    CHECK(!NormalizeRepoUrl("", &out, &why));
    CHECK(IsValidRepoName("my-fork", &why));
    CHECK(!IsValidRepoName("Official", &why));
    CHECK(!IsValidRepoName("a b", &why));
}

static void TestFiles() {
    namespace fs = std::filesystem;
    fs::path dir = fs::temp_directory_path() / "hom_test_dir";
    std::error_code ec;
    fs::remove_all(dir, ec);

    // repo config round trip; junk lines are skipped
    RepoConfig cfg;
    cfg.active = "mine";
    cfg.repos.push_back({"mine", "https://example.org/p/"});
    CHECK(SaveRepoConfig(dir / "cfg" / "hom_repos.cfg", cfg));
    {
        std::ofstream f(dir / "cfg" / "hom_repos.cfg", std::ios::app);
        f << "repo bad http://evil/\nrepo Mine https://dup.example/\nrepo ok2 someone/hom\n";
    }
    RepoConfig back = LoadRepoConfig(dir / "cfg" / "hom_repos.cfg");
    CHECK(back.active == "mine");
    CHECK(back.repos.size() == 2);
    CHECK(ActiveRepo(back).url == "https://example.org/p/");

    // install database: a hostile version must not shift the fields
    std::vector<DbRec> db = { {"a", "file", "1|C:\\Windows\\x", "D:\\files\\a.mp4"}, {"b", "archive", "2", "D:\\apps\\b"} };
    CHECK(SaveDb(dir / "db.cfg", db));
    std::vector<DbRec> got = LoadDb(dir / "db.cfg");
    CHECK(got.size() == 2);
    CHECK(got.size() == 2 && got[0].path == "D:\\files\\a.mp4" && got[0].version == "1_C:\\Windows\\x");
    {
        std::ofstream f(dir / "db.cfg", std::ios::app);
        f << "c|file|1|X|Y\nd|file\n";
    }
    CHECK(LoadDb(dir / "db.cfg").size() == 2); // malformed lines ignored

    // key/value settings
    std::map<std::string, std::string> kv = { {"homrec", "D:\\HomRec"}, {"data", "E:\\d"} };
    CHECK(SaveKv(dir / "hom.cfg", kv));
    CHECK(LoadKv(dir / "hom.cfg") == kv);
    fs::remove_all(dir, ec);
}

static void TestProgress() {
    CHECK(FormatBytes(512) == "512 B");
    CHECK(FormatBytes(1536) == "1.5 KB");
    CHECK(FormatBytes(3LL * 1024 * 1024) == "3.0 MB");
    CHECK(FormatBytes(5LL * 1024 * 1024 * 1024) == "5.00 GB");
    CHECK(FormatProgress(50, 1024 * 1024, 2 * 1024 * 1024, 4) == "[##########----------]  50%  1.0 MB / 2.0 MB  ~4s");
    CHECK(FormatProgress(-1, 2048, 0, -1) == "downloading...  2.0 KB");
    CHECK(FormatProgress(100, 10, 10, 75).find("~1m 15s") != std::string::npos);
}

int main() {
    TestNames();
    TestVersion();
    TestJson();
    TestUrls();
    TestFiles();
    TestProgress();
    if (g_fail) { std::printf("%d check(s) failed\n", g_fail); return 1; }
    std::printf("all checks passed\n");
    return 0;
}
