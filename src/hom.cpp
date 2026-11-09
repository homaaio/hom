// hom.cpp - "hom", a small package manager: HomRec plugins (.hrp/.jar), plain files,
// archives (.zip) and programs (.msi/.exe).  (v0.6.1)
//
// hom works on its own (no HomRec needed). Only `plugin` packages need a HomRec folder.
// Where things go:
//   plugin   -> <HomRec>\plugins\<file>
//   file     -> <data>\files\<name>\<file>        (or --to <dir>)
//   archive  -> <data>\apps\<name>\               (.zip, unpacked with Windows' tar.exe)
//   program  -> <data>\dl\<file>, then run (asks first)
// <data> is %LOCALAPPDATA%\hom, or <hom.exe folder>\hom_data when hom sits next to HomRec.
// Run `hom where` to see the folders hom picked.

#ifdef _WIN32
  #define WIN32_LEAN_AND_MEAN
  #define NOMINMAX
  #include <windows.h>
  #include <winhttp.h>
  #include <shlwapi.h>
  #include <dbghelp.h>
  #include <bcrypt.h>
  #include <shellapi.h>
  #if defined(_MSC_VER)
    // #pragma comment(lib, ...) is an MSVC extension; MinGW/GCC ignores it
    // with a warning and links via -lwinhttp -lshlwapi on the command line
    // instead (see the hom target in CMakeLists.txt).
    #pragma comment(lib, "winhttp.lib")
    #pragma comment(lib, "shlwapi.lib")
    #pragma comment(lib, "bcrypt.lib")
    #pragma comment(lib, "shell32.lib")
  #endif
#endif

#include <cstdio>
#include <cstring>
#include <ctime>
#include <cctype>
#include <string>
#include <vector>
#include <sstream>
#include <iostream>
#include <algorithm>
#include <fstream>
#include <regex>
#include <chrono>
#include <functional>
#include <map>

#include "common.h"

// -----------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------

static constexpr char k_hom_version[] = "0.6.1";

// If an install/update would grow disk usage by at least this much, CmdInstall() asks for
// confirmation first (Y/n) instead of just doing it - unless -y or -f/--force was passed.
static constexpr long long kDiskSpaceWarnBytes = 1LL * 1024 * 1024;

// The repo packages are served from: a folder URL (always https, always ends with '/') that holds
// index.json and the package files. hom itself is updated from homcommon::kSelf*Url, not from here.
static std::wstring       g_repo_base   = L"https://raw.githubusercontent.com/homaaio/hom/main/pkgs/";
static std::wstring       g_file_prefix;                       // L"plugins/" for an old-layout repo
static std::string        g_repo_name   = homcommon::kOfficialName;
static std::string        g_repo_url    = homcommon::kOfficialUrl;

// Folders (see InitPaths()). g_homrec is "" when no HomRec folder was found.
static std::string        g_state;   // hom.cfg, hom_repos.cfg, hom_db.cfg
static std::string        g_data;    // files, apps, dl (sub-folders)
static std::string        g_homrec;

// How the download progress is reported (see ProgressReporter below).
enum class ProgressMode { None, Bar, Lines };
static ProgressMode g_progress_mode = ProgressMode::None;

// -----------------------------------------------------------------------
// Small helpers shared by every command
// -----------------------------------------------------------------------

namespace {

// Usage text goes to stderr when it is shown because of a mistake (unknown command,
// missing argument) and to stdout when it was asked for (--help), like every
// other CLI does - so `hom --help | more` and the in-app console both see it.
void PrintUsage(FILE *out = stderr) {
    std::fprintf(out,
        "hom %s -- package manager for HomRec plugins, files, archives and programs\n\n"
        "Usage:\n"
        "  hom                              Open the interactive TUI (in a terminal)\n"
        "  hom tui                          Same, explicitly\n"
        "  hom --version                    Show the hom version\n"
        "  hom update [-f|--force]          Update hom itself (from github.com/homaaio/hom)\n"
        "  hom ping                         Check connectivity to the package repo\n"
        "  hom where                        Show the folders and the repo hom uses\n"
        "  hom set <key> [value]            homrec = HomRec folder, data = hom's data folder\n"
        "                                      (value 'auto' resets, no value shows it)\n"
        "  hom upgrade [-y] [-f]            Update installed packages that have a newer version\n"
        "  hom full-upgrade [-y] [-f]       Same as upgrade for now (no dependencies yet)\n"
        "  hom install <name> [-y] [-f] [--to <dir>] [--run]\n"
        "                                   Download and install a package\n"
        "                                      -y/-f  skip the disk-space question\n"
        "                                      --to   folder for a 'file' or 'archive' package\n"
        "                                      --run  run a 'program' installer without asking\n"
        "  hom remove <name> -r             Remove a package, keep a plugin's saved settings\n"
        "  hom purge <name> -r              Remove a package and a plugin's saved settings\n"
        "  hom autoremove                   Clean up orphaned auto-installed packages (no-op)\n"
        "  hom clean                        Delete downloaded installers/archives (data\\dl)\n"
        "  hom search <query>               Search package names/descriptions\n"
        "  hom show <name>                  Show details about a package\n"
        "  hom list --installed             List what is installed here\n"
        "  hom list --upgradable            List installed packages with a newer version\n"
        "  hom repo [list]                  Show the package repositories ('*' = in use)\n"
        "  hom repo add <n> <url|owner/repo[@branch]>\n"
        "                                   Remember a fork/mirror (https only)\n"
        "  hom repo set <n>                 Download from that repo from now on\n"
        "                                      ('hom repo set official' switches back)\n"
        "  hom repo remove <n>              Forget a repo\n\n"
        "Options (any command):\n"
        "  --progress=bar|lines|none        How downloads report progress\n"
        "                                      (default: bar in a terminal, none when piped)\n\n"
        "Examples:\n"
        "  hom search overlay\n"
        "  hom install input-overlay\n"
        "  hom install some-video --to D:\\clips\n"
        "  hom set homrec D:\\Apps\\HomRec\n"
        "  hom upgrade\n"
        "  hom remove input-overlay -r\n"
        "  hom repo add myfork someone/hom\n",
        k_hom_version);
}

#ifdef _WIN32

std::wstring Widen(const std::string &s) {
    if (s.empty()) return {};
    int len = MultiByteToWideChar(CP_UTF8, 0, s.c_str(), -1, nullptr, 0);
    std::wstring w(len > 0 ? len - 1 : 0, L'\0');
    if (len > 1) MultiByteToWideChar(CP_UTF8, 0, s.c_str(), -1, w.data(), len);
    return w;
}

// Recursively creates dir_path and any missing parents. Same pattern as
// hr_archive.cpp's CreateDirRecursive -- kept local here since hom.exe is
// a standalone binary and doesn't link against the rest of src/.
bool CreateDirRecursive(const std::string &dir_path) {
    if (dir_path.empty()) return false;
    DWORD attrs = GetFileAttributesA(dir_path.c_str());
    if (attrs != INVALID_FILE_ATTRIBUTES) return (attrs & FILE_ATTRIBUTE_DIRECTORY) != 0;

    size_t pos = dir_path.find_last_of("\\/");
    if (pos != std::string::npos) {
        if (!CreateDirRecursive(dir_path.substr(0, pos))) return false;
    }
    return CreateDirectoryA(dir_path.c_str(), nullptr) != 0 ||
           GetLastError() == ERROR_ALREADY_EXISTS;
}

// Recursively deletes dir_path (files + subfolders + itself).
bool DeleteDirRecursive(const std::string &dir_path) {
    DWORD attrs = GetFileAttributesA(dir_path.c_str());
    if (attrs == INVALID_FILE_ATTRIBUTES) return true; // nothing there, fine
    if (!(attrs & FILE_ATTRIBUTE_DIRECTORY)) return DeleteFileA(dir_path.c_str()) != 0;

    WIN32_FIND_DATAA fd;
    std::string pattern = dir_path + "\\*";
    HANDLE h = FindFirstFileA(pattern.c_str(), &fd);
    if (h != INVALID_HANDLE_VALUE) {
        do {
            std::string name = fd.cFileName;
            if (name == "." || name == "..") continue;
            std::string child = dir_path + "\\" + name;
            if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) {
                if (!DeleteDirRecursive(child)) { FindClose(h); return false; }
            } else {
                if (!DeleteFileA(child.c_str())) { FindClose(h); return false; }
            }
        } while (FindNextFileA(h, &fd));
        FindClose(h);
    }
    return RemoveDirectoryA(dir_path.c_str()) != 0;
}

// Same idea as DeleteDirRecursive, but leaves one top-level file alone.
bool DeleteDirContentsExcept(const std::string &dir_path, const std::string &keep_filename) {
    WIN32_FIND_DATAA fd;
    std::string pattern = dir_path + "\\*";
    HANDLE h = FindFirstFileA(pattern.c_str(), &fd);
    if (h == INVALID_HANDLE_VALUE) return true; // nothing there, fine
    bool ok = true;
    do {
        std::string name = fd.cFileName;
        if (name == "." || name == "..") continue;
        if (!(fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) && name == keep_filename) continue;
        std::string child = dir_path + "\\" + name;
        if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) {
            if (!DeleteDirRecursive(child)) ok = false;
        } else {
            if (!DeleteFileA(child.c_str())) ok = false;
        }
    } while (FindNextFileA(h, &fd));
    FindClose(h);
    return ok;
}

bool FileExistsA(const std::string &path) {
    DWORD attrs = GetFileAttributesA(path.c_str());
    return attrs != INVALID_FILE_ATTRIBUTES && !(attrs & FILE_ATTRIBUTE_DIRECTORY);
}

bool DirExistsA(const std::string &path) {
    DWORD attrs = GetFileAttributesA(path.c_str());
    return attrs != INVALID_FILE_ATTRIBUTES && (attrs & FILE_ATTRIBUTE_DIRECTORY) != 0;
}

std::string Trim(std::string s) {
    size_t a = s.find_first_not_of(" \t\r\n");
    if (a == std::string::npos) return "";
    size_t b = s.find_last_not_of(" \t\r\n");
    return s.substr(a, b - a + 1);
}

long long FileSizeA(const std::string &path) {
    WIN32_FILE_ATTRIBUTE_DATA fad{};
    if (!GetFileAttributesExA(path.c_str(), GetFileExInfoStandard, &fad)) return 0;
    ULARGE_INTEGER sz;
    sz.HighPart = fad.nFileSizeHigh;
    sz.LowPart = fad.nFileSizeLow;
    return (long long)sz.QuadPart;
}

bool IsSafePluginName(const std::string &name, std::string *reason) {
    if (name.empty()) { *reason = "name is empty"; return false; }
    if (name.find("..") != std::string::npos)  { *reason = "contains '..'"; return false; }
    if (name.find('/')  != std::string::npos)  { *reason = "contains '/'";  return false; }
    if (name.find('\\') != std::string::npos)  { *reason = "contains '\\'"; return false; }
    if (name.find(':')  != std::string::npos)  { *reason = "contains ':'";  return false; }
    for (unsigned char c : name)
        if (c < 0x20 || c == '"' || c == '<' || c == '>' || c == '|' || c == '*' || c == '?') {
            *reason = "contains an illegal character"; return false;
        }
    return true;
}

// -- Paths, hashes, install database -----------------------------------------

std::string PluginsDir() { return g_homrec.empty() ? std::string() : g_homrec + "\\plugins"; }

std::string Lower(std::string s) {
    std::transform(s.begin(), s.end(), s.begin(), [](unsigned char c) { return (char)std::tolower(c); });
    return s;
}

// A folder name from an index entry ("dest"): relative, no '..', no drive. Result uses '\\'.
bool IsSafeRel(const std::string &rel, std::string *out) {
    if (rel.empty() || rel.size() > 120) return false;
    std::string r = rel;
    for (char &c : r) if (c == '/') c = '\\';
    if (r.front() == '\\' || r.back() == '\\' || r.find("..") != std::string::npos || r.find(':') != std::string::npos ||
        r.find("\\\\") != std::string::npos) return false;
    for (unsigned char c : r)
        if (c < 0x20 || c == '"' || c == '<' || c == '>' || c == '|' || c == '*' || c == '?') return false;
    *out = r;
    return true;
}

// Percent-encodes what isn't safe in a URL path (file names with spaces, non-ASCII...).
std::string PctEncode(const std::string &s) {
    static const char *hx = "0123456789ABCDEF";
    std::string o;
    for (unsigned char c : s) {
        if (std::isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~' || c == '/') o.push_back((char)c);
        else { o.push_back('%'); o.push_back(hx[c >> 4]); o.push_back(hx[c & 15]); }
    }
    return o;
}

bool IsUnder(const std::string &path, const std::string &base) {
    namespace fs = std::filesystem;
    std::error_code ec;
    fs::path a = fs::weakly_canonical(fs::path(path), ec);
    if (ec) return false;
    fs::path b = fs::weakly_canonical(fs::path(base), ec);
    if (ec) return false;
    std::string sa = Lower(a.string()), sb = Lower(b.string());
    if (!sb.empty() && sb.back() != '\\') sb.push_back('\\');
    return sa.size() > sb.size() && sa.compare(0, sb.size(), sb) == 0;
}

// SHA-256 through Windows CNG (bcrypt.dll), fed in pieces so big downloads never sit in memory.
class Sha256 {
public:
    Sha256() {
        if (BCryptOpenAlgorithmProvider(&alg_, BCRYPT_SHA256_ALGORITHM, nullptr, 0) != 0) { alg_ = nullptr; return; }
        DWORD obj = 0, got = 0;
        BCryptGetProperty(alg_, BCRYPT_OBJECT_LENGTH, (PUCHAR)&obj, sizeof(obj), &got, 0);
        buf_.resize(obj ? obj : 1024);
        if (BCryptCreateHash(alg_, &h_, buf_.data(), (ULONG)buf_.size(), nullptr, 0, 0) != 0) h_ = nullptr;
    }
    ~Sha256() {
        if (h_) BCryptDestroyHash(h_);
        if (alg_) BCryptCloseAlgorithmProvider(alg_, 0);
    }
    Sha256(const Sha256 &) = delete;
    Sha256 &operator=(const Sha256 &) = delete;
    void Add(const char *p, size_t n) { if (h_ && n) BCryptHashData(h_, (PUCHAR)p, (ULONG)n, 0); }
    std::string Hex() { // "" if CNG failed
        if (!h_) return {};
        UCHAR d[32];
        if (BCryptFinishHash(h_, d, sizeof(d), 0) != 0) return {};
        static const char *hx = "0123456789abcdef";
        std::string s;
        for (UCHAR c : d) { s.push_back(hx[c >> 4]); s.push_back(hx[c & 15]); }
        return s;
    }
private:
    BCRYPT_ALG_HANDLE  alg_ = nullptr;
    BCRYPT_HASH_HANDLE h_   = nullptr;
    std::vector<UCHAR> buf_;
};

std::string Sha256Of(const std::string &data) {
    Sha256 h;
    h.Add(data.data(), data.size());
    return h.Hex();
}

// What hom installed (see common.h's DbRec). Plugins are also found by scanning plugins\, the
// database adds their version and tracks everything else.
std::filesystem::path DbPath() { return std::filesystem::path(g_state) / homcommon::kDbName; }

std::vector<homcommon::DbRec> DbLoad() { return homcommon::LoadDb(DbPath()); }

const homcommon::DbRec *DbFind(const std::vector<homcommon::DbRec> &db, const std::string &name) {
    for (const auto &r : db) if (r.name == name) return &r;
    return nullptr;
}

void DbPut(const homcommon::DbRec &rec) {
    std::vector<homcommon::DbRec> db = DbLoad();
    db.erase(std::remove_if(db.begin(), db.end(), [&](const homcommon::DbRec &r) { return r.name == rec.name; }), db.end());
    db.push_back(rec);
    homcommon::SaveDb(DbPath(), db);
}

void DbDel(const std::string &name) {
    std::vector<homcommon::DbRec> db = DbLoad();
    size_t before = db.size();
    db.erase(std::remove_if(db.begin(), db.end(), [&](const homcommon::DbRec &r) { return r.name == name; }), db.end());
    if (db.size() != before) homcommon::SaveDb(DbPath(), db);
}

// -- Tiny JSON helpers -----------------------------------------------------

std::string ExtractJsonString(const std::string &json, const std::string &key,
                               size_t from = 0) {
    std::string needle = "\"" + key + "\"";
    size_t pos = json.find(needle, from);
    if (pos == std::string::npos) return {};
    pos = json.find(':', pos);
    if (pos == std::string::npos) return {};
    pos = json.find('"', pos);
    if (pos == std::string::npos) return {};

    // Walk the value instead of taking everything up to the next quote:
    // plugin descriptions contain escaped quotes (\"...\"), and a plain
    // find('"') would stop at the backslash.
    std::string out;
    auto hex4 = [&](size_t at, unsigned *v) -> bool {
        if (at + 4 > json.size()) return false;
        unsigned n = 0;
        for (size_t k = 0; k < 4; ++k) {
            char h = json[at + k];
            n <<= 4;
            if (h >= '0' && h <= '9') n |= (unsigned)(h - '0');
            else if (h >= 'a' && h <= 'f') n |= (unsigned)(h - 'a' + 10);
            else if (h >= 'A' && h <= 'F') n |= (unsigned)(h - 'A' + 10);
            else return false;
        }
        *v = n;
        return true;
    };
    for (size_t i = pos + 1; i < json.size(); ++i) {
        char c = json[i];
        if (c == '"') return out;
        if (c != '\\') { out.push_back(c); continue; }
        if (++i >= json.size()) break;
        switch (json[i]) {
            case '"':  out.push_back('"');  break;
            case '\\': out.push_back('\\'); break;
            case '/':  out.push_back('/');  break;
            case 'b':  out.push_back('\b'); break;
            case 'f':  out.push_back('\f'); break;
            case 'n':  out.push_back('\n'); break;
            case 'r':  out.push_back('\r'); break;
            case 't':  out.push_back('\t'); break;
            case 'u': {
                unsigned cp = 0;
                if (!hex4(i + 1, &cp)) break;
                i += 4;
                if (cp >= 0xD800 && cp <= 0xDBFF && i + 6 < json.size() &&
                    json[i + 1] == '\\' && json[i + 2] == 'u') {
                    unsigned lo = 0;
                    if (hex4(i + 3, &lo) && lo >= 0xDC00 && lo <= 0xDFFF) {
                        cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                        i += 6;
                    }
                }
                if (cp < 0x80) {
                    out.push_back((char)cp);
                } else if (cp < 0x800) {
                    out.push_back((char)(0xC0 | (cp >> 6)));
                    out.push_back((char)(0x80 | (cp & 0x3F)));
                } else if (cp < 0x10000) {
                    out.push_back((char)(0xE0 | (cp >> 12)));
                    out.push_back((char)(0x80 | ((cp >> 6) & 0x3F)));
                    out.push_back((char)(0x80 | (cp & 0x3F)));
                } else {
                    out.push_back((char)(0xF0 | (cp >> 18)));
                    out.push_back((char)(0x80 | ((cp >> 12) & 0x3F)));
                    out.push_back((char)(0x80 | ((cp >> 6) & 0x3F)));
                    out.push_back((char)(0x80 | (cp & 0x3F)));
                }
                break;
            }
            default: out.push_back(json[i]); break;
        }
    }
    return {};
}

// One entry from Hom/plugins/index.json's "plugins" array.
struct IndexEntry {
    std::string name;
    std::string file;
    std::string description;
    std::string version;
    std::string author; // may be empty - not every plugin.json sets one
    // Package fields (all optional; an old index with only the fields above means "plugin"):
    std::string type;   // plugin (default) | file | archive | program
    std::string url;    // absolute https URL (e.g. a GitHub Release asset); default: <repo>/<file>
    std::string sha256; // hex; required for 'program' and 'archive'... see InstallPkg()
    std::string dest;   // 'file'/'archive': sub-folder under hom's data folder
    std::string args;   // 'program': command-line arguments for the installer
};

std::vector<IndexEntry> ParsePluginIndex(const std::string &json) {
    std::vector<IndexEntry> out;
    size_t arr_key = json.find("\"packages\"");
    if (arr_key == std::string::npos) arr_key = json.find("\"plugins\""); // old index layout
    if (arr_key == std::string::npos) return out;
    size_t arr_start = json.find('[', arr_key);
    if (arr_start == std::string::npos) return out;

    int depth = 0;
    size_t obj_start = std::string::npos;
    for (size_t i = arr_start; i < json.size(); ++i) {
        char c = json[i];
        if (c == '{') {
            if (depth == 0) obj_start = i;
            ++depth;
        } else if (c == '}') {
            --depth;
            if (depth == 0 && obj_start != std::string::npos) {
                std::string obj = json.substr(obj_start, i - obj_start + 1);
                IndexEntry e;
                e.name        = ExtractJsonString(obj, "name");
                e.file        = ExtractJsonString(obj, "file");
                e.description = ExtractJsonString(obj, "description");
                e.version     = ExtractJsonString(obj, "version");
                e.author      = ExtractJsonString(obj, "author");
                e.type        = Lower(ExtractJsonString(obj, "type"));
                e.url         = ExtractJsonString(obj, "url");
                e.sha256      = Lower(ExtractJsonString(obj, "sha256"));
                e.dest        = ExtractJsonString(obj, "dest");
                e.args        = ExtractJsonString(obj, "args");
                if (!e.name.empty()) out.push_back(std::move(e));
                obj_start = std::string::npos;
            }
        } else if (c == ']' && depth == 0) {
            break; // end of the plugins array
        }
    }
    return out;
}

// -- Local packages ---------------------------------------------------------
std::string LocalPluginVersion(const std::string &name) {
    if (!g_homrec.empty()) {
        for (const std::string &dir : { PluginsDir() + "\\.installed\\" + name, PluginsDir() + "\\" + name }) {
            std::string manifest_path = dir + "\\plugin.json";
            std::ifstream f(manifest_path, std::ios::binary);
            if (!f) continue;
            std::ostringstream ss;
            ss << f.rdbuf();
            std::string v = ExtractJsonString(ss.str(), "version");
            if (!v.empty()) return v;
        }
    }
    // HomRec hasn't extracted it yet: use the version hom wrote down at install time.
    std::vector<homcommon::DbRec> db = DbLoad();
    if (const homcommon::DbRec *r = DbFind(db, name)) return r->version;
    return "";
}

struct LocalPlugin {
    std::string name;
    std::string kind;    // "hrp", "jar", "folder" (plugins) or the package type (file/archive/program)
    std::string version; // "" if unknown (see LocalPluginVersion above)
};

std::vector<LocalPlugin> ListLocalPlugins() {
    std::vector<LocalPlugin> out;
    if (!g_homrec.empty()) {
        WIN32_FIND_DATAA fd;
        HANDLE h = FindFirstFileA((PluginsDir() + "\\*").c_str(), &fd);
        if (h != INVALID_HANDLE_VALUE) {
            do {
                std::string name = fd.cFileName;
                if (name == "." || name == "..") continue;
                if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) {
                    if (name == ".installed") continue; // extraction cache, not a plugin itself
                    std::ifstream manifest(PluginsDir() + "\\" + name + "\\plugin.json");
                    if (!manifest) continue; // not actually a plugin folder
                    LocalPlugin p;
                    p.name = name;
                    p.kind = "folder";
                    p.version = LocalPluginVersion(name);
                    out.push_back(std::move(p));
                } else if (name.size() > 4 && _stricmp(name.c_str() + name.size() - 4, ".hrp") == 0) {
                    LocalPlugin p;
                    p.name = name.substr(0, name.size() - 4);
                    p.kind = "hrp";
                    p.version = LocalPluginVersion(p.name);
                    out.push_back(std::move(p));
                } else if (name.size() > 4 && _stricmp(name.c_str() + name.size() - 4, ".jar") == 0) {
                    LocalPlugin p;
                    p.name = name.substr(0, name.size() - 4);
                    p.kind = "jar";
                    p.version = LocalPluginVersion(p.name);
                    out.push_back(std::move(p));
                }
            } while (FindNextFileA(h, &fd));
            FindClose(h);
        }
    }
    for (const auto &r : DbLoad()) {
        if (r.type == "plugin") continue; // found by the scan above
        LocalPlugin p;
        p.name = r.name;
        p.kind = r.type;
        p.version = r.version;
        out.push_back(std::move(p));
    }
    return out;
}

// Collected command-line switches shared by update/install/remove.
struct Flags {
    bool force = false; // -f / --force
    bool yes   = false; // -y  - auto-answer "yes" to any Y/n prompt
    bool rflag = false; // -r  - required to confirm a destructive remove
    bool run   = false; // --run - run a 'program' installer without asking
    std::string to;     // --to <dir> - folder for 'file' / 'archive' packages
};

Flags ParseHomFlags(int argc, char **argv, int start, std::string *positional) {
    Flags f;
    positional->clear();
    for (int i = start; i < argc; ++i) {
        std::string a = argv[i];
        if (a == "-f" || a == "--force") f.force = true;
        else if (a == "-y") f.yes = true;
        else if (a == "-r") f.rflag = true;
        else if (a == "--run") f.run = true;
        else if (a == "--to" && i + 1 < argc) f.to = argv[++i];
        else if (positional->empty()) *positional = a; // first non-flag token
    }
    return f;
}

// Prints `prompt` + " (Y/n) " and reads one line from stdin. Empty input
// (just pressing Enter) counts as yes, matching the usual shell "Y/n"
// convention where the capital letter is the default.
bool ConfirmYesNo(const std::string &prompt) {
    std::printf("%s (Y/n) ", prompt.c_str());
    std::fflush(stdout);
    std::string line;
    if (!std::getline(std::cin, line)) return true; // no stdin (e.g. piped) -> don't block forever
    line = Trim(line);
    if (line.empty()) return true;
    char c = (char)std::tolower((unsigned char)line[0]);
    return c == 'y';
}

// -- Networking (WinHTTP, HTTPS GET) -----------------------------------------

struct FetchResult {
    bool        ok = false;     // request completed, got *a* response
    int         status = 0;     // HTTP status code, e.g. 200, 404
    std::string body;           // raw response bytes (empty for a HEAD request)
    long long   content_length = -1; // from Content-Length header, -1 if absent
};

// -- Download progress --------------------------------------------------------
class ProgressReporter {
public:
    explicit ProgressReporter(ProgressMode mode)
        : mode_(mode), start_(std::chrono::steady_clock::now()), last_emit_(start_) {}

    void Update(long long received, long long total) {
        if (mode_ == ProgressMode::None) return;
        auto now = std::chrono::steady_clock::now();
        bool final = total > 0 && received >= total;
        if (emitted_ && !final && now - last_emit_ < std::chrono::milliseconds(100)) return;
        last_emit_ = now;
        emitted_ = true;

        double elapsed = std::chrono::duration<double>(now - start_).count();
        int pct = total > 0 ? (int)std::min<long long>(100, received * 100 / total) : -1;
        int eta = -1;
        if (!final && total > 0 && received > 0 && elapsed >= 0.5) {
            double speed = (double)received / elapsed;
            eta = (int)(((double)(total - received)) / speed + 0.5);
        }

        if (mode_ == ProgressMode::Lines) {
            std::printf("@progress %d %lld %lld %d\n", pct, received, total, eta);
            std::fflush(stdout);
        } else {
            std::string line = "  " + homcommon::FormatProgress(pct, received, total, eta);
            size_t len = line.size();
            if (len < last_len_) line.append(last_len_ - len, ' '); // wipe leftovers of a longer previous line
            last_len_ = len;
            std::printf("\r%s", line.c_str());
            std::fflush(stdout);
            drew_ = true;
        }
    }

    // Ends the redrawn line (bar mode only) so the next printf starts fresh.
    void Finish() {
        if (mode_ == ProgressMode::Bar && drew_) { std::printf("\n"); std::fflush(stdout); }
        drew_ = false;
    }

private:
    ProgressMode mode_;
    std::chrono::steady_clock::time_point start_, last_emit_;
    bool   emitted_ = false;
    bool   drew_ = false;
    size_t last_len_ = 0;
};

// Receives the body in pieces; return false to stop the download.
using Sink = std::function<bool(const char *, size_t)>;

constexpr long long kMaxMemBody  = 64LL * 1024 * 1024;        // when the body is collected in memory
constexpr long long kMaxDownload = 4LL * 1024 * 1024 * 1024;  // when it goes to a file (via a Sink)

// GET/HEAD any https URL. WinHTTP follows redirects, so GitHub Release links
// (github.com -> objects.githubusercontent.com) work. Without a `sink` the body is collected in
// FetchResult::body (64 MB cap); with one it is handed over piece by piece, 200 responses only.
FetchResult FetchUrl(const std::wstring &url, const wchar_t *method = L"GET",
                     ProgressReporter *progress = nullptr, const Sink &sink = nullptr) {
    FetchResult out;

    URL_COMPONENTS uc = {};
    uc.dwStructSize = sizeof(uc);
    wchar_t host[256] = {}, path[2048] = {}, extra[1024] = {};
    uc.lpszHostName = host;  uc.dwHostNameLength = _countof(host);
    uc.lpszUrlPath  = path;  uc.dwUrlPathLength  = _countof(path);
    uc.lpszExtraInfo = extra; uc.dwExtraInfoLength = _countof(extra);
    if (!WinHttpCrackUrl(url.c_str(), (DWORD)url.size(), 0, &uc)) return out;
    if (uc.nScheme != INTERNET_SCHEME_HTTPS) return out; // never plain http
    std::wstring full_path = std::wstring(path) + extra;

    HINTERNET hSession = WinHttpOpen(
        L"hom-package-manager/1.0",
        WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
        WINHTTP_NO_PROXY_NAME, WINHTTP_NO_PROXY_BYPASS, 0);
    if (!hSession) return out;

    HINTERNET hConnect = WinHttpConnect(hSession, host, uc.nPort, 0);
    if (!hConnect) { WinHttpCloseHandle(hSession); return out; }

    HINTERNET hReq = WinHttpOpenRequest(
        hConnect, method, full_path.c_str(),
        nullptr, WINHTTP_NO_REFERER, WINHTTP_DEFAULT_ACCEPT_TYPES,
        WINHTTP_FLAG_SECURE);
    if (!hReq) {
        WinHttpCloseHandle(hConnect); WinHttpCloseHandle(hSession);
        return out;
    }

    if (!WinHttpSendRequest(hReq, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                             WINHTTP_NO_REQUEST_DATA, 0, 0, 0) ||
        !WinHttpReceiveResponse(hReq, nullptr)) {
        WinHttpCloseHandle(hReq); WinHttpCloseHandle(hConnect); WinHttpCloseHandle(hSession);
        return out;
    }

    DWORD status = 0, status_size = sizeof(status);
    WinHttpQueryHeaders(hReq, WINHTTP_QUERY_FLAG_NUMBER | WINHTTP_QUERY_STATUS_CODE,
                        WINHTTP_HEADER_NAME_BY_INDEX, &status, &status_size, WINHTTP_NO_HEADER_INDEX);
    out.status = (int)status;
    out.ok = true;

    // As text, not DWORD: a package can be bigger than 4 GB in theory.
    wchar_t clen[32] = {};
    DWORD clen_size = sizeof(clen);
    if (WinHttpQueryHeaders(hReq, WINHTTP_QUERY_CONTENT_LENGTH,
                            WINHTTP_HEADER_NAME_BY_INDEX, clen, &clen_size, WINHTTP_NO_HEADER_INDEX)) {
        out.content_length = _wtoi64(clen);
    }

    if (wcscmp(method, L"HEAD") != 0) {
        // Only report progress for a real download, not for a 404 page.
        ProgressReporter *prog = (status == 200) ? progress : nullptr;
        if (prog) prog->Update(0, out.content_length);
        char buf[16384];
        DWORD read = 0;
        long long received = 0;
        while (WinHttpReadData(hReq, buf, sizeof(buf), &read) && read > 0) {
            received += read;
            if (sink) {
                if (status == 200 && !sink(buf, read)) { out.ok = false; break; }
                if (received > kMaxDownload) { out.ok = false; break; }
            } else {
                out.body.append(buf, read);
                if ((long long)out.body.size() > kMaxMemBody) break;
            }
            if (prog) prog->Update(received, out.content_length);
        }
    }

    WinHttpCloseHandle(hReq);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);
    return out;
}

// `rel` is relative to the repo folder (g_repo_base). `method` defaults to GET.
FetchResult FetchFromRepo(const std::wstring &rel, const wchar_t *method = L"GET",
                          ProgressReporter *progress = nullptr) {
    return FetchUrl(g_repo_base + rel, method, progress);
}

// Streams a download into `dest` (written as dest + ".part", moved into place on success) and
// hashes it on the way. The whole file never sits in memory.
struct DlResult {
    bool        ok = false;
    int         status = 0;
    long long   size = 0;
    std::string sha;     // hex of what was written
};

DlResult DownloadToFile(const std::wstring &url, const std::string &dest, ProgressReporter *progress) {
    DlResult r;
    size_t cut = dest.find_last_of("\\/");
    if (cut != std::string::npos && !CreateDirRecursive(dest.substr(0, cut))) return r;

    const std::string part = dest + ".part";
    std::ofstream f(part, std::ios::binary | std::ios::trunc);
    if (!f) return r;
    Sha256 hash;
    long long written = 0;
    bool write_ok = true;
    FetchResult res = FetchUrl(url, L"GET", progress, [&](const char *p, size_t n) {
        f.write(p, (std::streamsize)n);
        if (!f) { write_ok = false; return false; }
        hash.Add(p, n);
        written += (long long)n;
        return true;
    });
    f.close();
    r.status = res.status;
    if (!res.ok || res.status != 200 || !write_ok || written <= 0) {
        DeleteFileA(part.c_str());
        return r;
    }
    if (res.content_length >= 0 && res.content_length != written) { // cut short
        DeleteFileA(part.c_str());
        return r;
    }
    if (!MoveFileExA(part.c_str(), dest.c_str(), MOVEFILE_REPLACE_EXISTING | MOVEFILE_COPY_ALLOWED)) {
        DeleteFileA(part.c_str());
        return r;
    }
    r.ok = true;
    r.size = written;
    r.sha = hash.Hex();
    return r;
}

// Fetches and parses the repo's index.json. A repo in the old layout keeps it in plugins/.
std::vector<IndexEntry> FetchIndex(bool *ok) {
    *ok = false;
    g_file_prefix.clear();
    FetchResult res = FetchFromRepo(L"index.json");
    if (res.ok && res.status == 404) {
        res = FetchFromRepo(L"plugins/index.json");
        if (res.ok && res.status == 200) g_file_prefix = L"plugins/";
    }
    if (!res.ok || res.status != 200 || res.body.empty()) return {};
    *ok = true;
    return ParsePluginIndex(res.body);
}

bool WriteFileBytes(const std::string &path, const std::string &data) {
    std::ofstream f(path, std::ios::binary | std::ios::trunc);
    if (!f) return false;
    f.write(data.data(), (std::streamsize)data.size());
    return f.good();
}

// -- Version comparison (same rules as hr_update.cpp's _version_gt) -------

bool VersionGt(const std::string &a, const std::string &b) {
    auto parse = [](const std::string &s) -> std::vector<int> {
        std::vector<int> parts;
        std::stringstream ss(s);
        std::string tok;
        while (std::getline(ss, tok, '.'))
            parts.push_back(std::atoi(tok.c_str()));
        return parts;
    };
    auto va = parse(a);
    auto vb = parse(b);
    size_t n = std::max(va.size(), vb.size());
    va.resize(n, 0); vb.resize(n, 0);
    for (size_t i = 0; i < n; ++i) {
        if (va[i] > vb[i]) return true;
        if (va[i] < vb[i]) return false;
    }
    return false;
}

std::string ExeDir() {
    char path[MAX_PATH];
    DWORD n = GetModuleFileNameA(nullptr, path, MAX_PATH);
    if (n == 0 || n >= MAX_PATH) return ".";
    std::string p(path, n);
    size_t pos = p.find_last_of("\\/");
    return pos == std::string::npos ? "." : p.substr(0, pos);
}

std::string ExePath() {
    char path[MAX_PATH];
    DWORD n = GetModuleFileNameA(nullptr, path, MAX_PATH);
    if (n == 0 || n >= MAX_PATH) return "hom.exe";
    return std::string(path, n);
}

#endif // _WIN32

} // namespace

#ifdef _WIN32

int CmdVersion() {
    std::printf("hom version %s\n", k_hom_version);
    return 0;
}

// -- Install / remove building blocks ------------------------------------------

static const char kNoHomrecMsg[] =
    "this is a HomRec plugin, but no HomRec folder was found. "
    "Run hom from HomRec's folder, or tell it where HomRec is:  hom set homrec <folder>";

static bool LooksLikeHomrec(const std::string &d) {
    return !d.empty() && (FileExistsA(d + "\\hr.exe") || FileExistsA(d + "\\homrec.exe"));
}

// Works out hom's own folders and (if there is one) the HomRec folder. Called first thing in main().
//   settings/db: homcommon::StateDir()     data: see the header of this file
//   HomRec folder, first hit wins: `hom set homrec`, the current folder, hom.exe's folder,
//   %LOCALAPPDATA%\Programs\HomRec (the installer's default), %ProgramFiles%\HomRec
static void InitPaths() {
    namespace fs = std::filesystem;
    fs::path exe = ExeDir();
    fs::path state = homcommon::StateDir(exe);
    g_state = state.string();
    g_data  = (state.filename() == "cfg") ? (exe / "hom_data").string() : g_state;

    std::map<std::string, std::string> kv = homcommon::LoadKv(state / homcommon::kCfgName);
    if (!kv["data"].empty()) g_data = kv["data"];

    std::string h = kv["homrec"];
    if (h.empty()) {
        char cwd[MAX_PATH];
        DWORD n = GetCurrentDirectoryA(MAX_PATH, cwd);
        if (n > 0 && n < MAX_PATH && LooksLikeHomrec(cwd)) h = cwd;
    }
    if (h.empty() && LooksLikeHomrec(ExeDir())) h = ExeDir();
    if (h.empty()) {
        const char *la = std::getenv("LOCALAPPDATA");
        const char *pf = std::getenv("ProgramFiles");
        if (la && LooksLikeHomrec(std::string(la) + "\\Programs\\HomRec")) h = std::string(la) + "\\Programs\\HomRec";
        else if (pf && LooksLikeHomrec(std::string(pf) + "\\HomRec")) h = std::string(pf) + "\\HomRec";
    }
    while (h.size() > 3 && (h.back() == '\\' || h.back() == '/')) h.pop_back();
    g_homrec = h;
}

// Default is NO: used before running a downloaded installer (and on EOF, i.e. a pipe).
static bool ConfirmRun(const std::string &prompt) {
    std::printf("%s (y/N) ", prompt.c_str());
    std::fflush(stdout);
    std::string line;
    if (!std::getline(std::cin, line)) return false;
    line = Trim(line);
    return !line.empty() && std::tolower((unsigned char)line[0]) == 'y';
}

// Unpacks a .zip with the tar.exe that ships with Windows 10 (1803+). It refuses entries that
// climb out of the target folder ("..") unless told otherwise, which is why hom uses it.
static bool ExtractZip(const std::string &zip, const std::string &dir, std::string *err) {
    char sys[MAX_PATH];
    UINT n = GetSystemDirectoryA(sys, MAX_PATH);
    std::string tar = (n > 0 && n < MAX_PATH) ? std::string(sys) + "\\tar.exe" : std::string();
    if (tar.empty() || !FileExistsA(tar)) { *err = "tar.exe not found (needs Windows 10 version 1803 or newer)"; return false; }
    if (zip.find('"') != std::string::npos || dir.find('"') != std::string::npos) { *err = "bad path"; return false; }

    std::string cmd = "\"" + tar + "\" -xf \"" + zip + "\" -C \"" + dir + "\"";
    std::vector<char> buf(cmd.begin(), cmd.end());
    buf.push_back('\0');
    STARTUPINFOA si{};
    si.cb = sizeof(si);
    PROCESS_INFORMATION pi{};
    if (!CreateProcessA(nullptr, buf.data(), nullptr, nullptr, FALSE, CREATE_NO_WINDOW, nullptr, nullptr, &si, &pi)) {
        *err = "couldn't start tar.exe";
        return false;
    }
    WaitForSingleObject(pi.hProcess, INFINITE);
    DWORD code = 1;
    GetExitCodeProcess(pi.hProcess, &code);
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);
    if (code != 0) { *err = "tar.exe failed (exit code " + std::to_string(code) + ") - is it a valid .zip?"; return false; }
    return true;
}

// Opens an installer (.msi/.exe) the way Explorer would and waits for it.
static bool RunProgram(const std::string &path, const std::string &args, DWORD *code) {
    SHELLEXECUTEINFOA sei{};
    sei.cbSize = sizeof(sei);
    sei.fMask = SEE_MASK_NOCLOSEPROCESS;
    sei.lpVerb = "open";
    sei.lpFile = path.c_str();
    sei.lpParameters = args.empty() ? nullptr : args.c_str();
    sei.nShow = SW_SHOWNORMAL;
    if (!ShellExecuteExA(&sei)) return false;
    *code = 0;
    if (sei.hProcess) {
        WaitForSingleObject(sei.hProcess, INFINITE);
        GetExitCodeProcess(sei.hProcess, code);
        CloseHandle(sei.hProcess);
    }
    return true;
}

static std::string PkgType(const IndexEntry &e) { return e.type.empty() ? "plugin" : e.type; }

// The file a package is downloaded from: its own https URL, or <repo>/<file>.
static std::wstring PkgUrl(const IndexEntry &e, std::string *err) {
    std::string file = e.file.empty() ? (e.name + ".hrp") : e.file;
    if (!e.url.empty()) {
        if (Lower(e.url).compare(0, 8, "https://") != 0) { *err = "the package URL must be https://"; return {}; }
        return Widen(e.url);
    }
    return g_repo_base + g_file_prefix + Widen(PctEncode(file));
}

struct InstallOpts {
    bool yes = false, force = false;   // skip the disk-space question
    bool run = false;                  // 'program': run the installer without asking
    bool interactive = false;          // may ask questions (false in the TUI)
    std::string to;                    // 'file'/'archive': folder chosen by the person
};

struct InstallInfo {
    bool ok = false, cancelled = false, existed = false, ran = false;
    int  status = 0;                   // HTTP status of a failed download
    long long size = 0;
    std::string type, dest, note;
};

// Downloads one package and puts it where its type says. Never prints (the TUI calls it too);
// problems come back in *msg. The download is checked against the index's sha256 *before* it
// replaces anything on disk.
static InstallInfo InstallPkg(const IndexEntry &e, const InstallOpts &o, ProgressReporter *prog, std::string *msg) {
    InstallInfo info;
    std::string reason;
    if (!IsSafePluginName(e.name, &reason)) {
        *msg = "refusing package name '" + e.name + "' (" + reason + ")";
        return info;
    }
    std::string type = PkgType(e);
    if (type != "plugin" && type != "file" && type != "archive" && type != "program") {
        *msg = "package '" + e.name + "' has type '" + type + "', which this hom doesn't know (try 'hom update').";
        return info;
    }
    info.type = type;

    std::string file = e.file.empty() ? (e.name + ".hrp") : e.file;
    if (!IsSafePluginName(file, &reason)) {
        *msg = "refusing file name '" + file + "' (" + reason + ")";
        return info;
    }

    // -- where it ends up
    std::string dest, dest_dir;
    if (type == "plugin") {
        std::string ext = file.size() >= 4 ? Lower(file.substr(file.size() - 4)) : "";
        if (ext != ".hrp" && ext != ".jar") { *msg = "a plugin file must be .hrp or .jar, not '" + file + "'"; return info; }
        if (g_homrec.empty()) { *msg = kNoHomrecMsg; return info; }
        dest = PluginsDir() + "\\" + e.name + ext;
    } else if (type == "file" || type == "archive") {
        if (!o.to.empty()) {
            char full[MAX_PATH];
            DWORD n = GetFullPathNameA(o.to.c_str(), MAX_PATH, full, nullptr);
            if (n == 0 || n >= MAX_PATH) { *msg = "bad --to folder"; return info; }
            dest_dir = full;
        } else {
            std::string rel = e.name;
            if (!e.dest.empty() && !IsSafeRel(e.dest, &rel)) { *msg = "bad 'dest' in the index: '" + e.dest + "'"; return info; }
            dest_dir = g_data + (type == "file" ? "\\files\\" : "\\apps\\") + rel;
        }
        dest = (type == "file") ? dest_dir + "\\" + file : dest_dir;
    }
    const std::string cache = g_data + "\\dl\\" + e.name + "__" + file;
    if (type == "program") dest = cache;

    std::wstring url = PkgUrl(e, &reason);
    if (url.empty()) { *msg = reason; return info; }

    info.existed = (type == "archive") ? DirExistsA(dest) : FileExistsA(dest);

    // -- big download? ask first (needs a HEAD request, so only when we can ask)
    if (o.interactive && !o.yes && !o.force) {
        FetchResult head = FetchUrl(url, L"HEAD");
        if (head.ok && head.status == 200 && head.content_length >= 0) {
            long long delta = head.content_length - ((type == "plugin" || type == "file") && info.existed ? FileSizeA(dest) : 0);
            if (delta >= kDiskSpaceWarnBytes) {
                char prompt[200];
                std::snprintf(prompt, sizeof(prompt),
                              "hom: installing '%s' will use about %.1f MB of additional disk space. Continue?",
                              e.name.c_str(), (double)delta / (1024.0 * 1024.0));
                if (!ConfirmYesNo(prompt)) { info.cancelled = true; *msg = "install cancelled."; return info; }
            }
        }
    }

    // -- download + verify
    DlResult dl = DownloadToFile(url, cache, prog);
    info.status = dl.status;
    if (!dl.ok) {
        if (dl.status == 404)      *msg = "'" + file + "' was not found in the repo (404).";
        else if (dl.status != 0)   *msg = "download failed (HTTP " + std::to_string(dl.status) + ").";
        else                       *msg = "network request failed - check your connection.";
        return info;
    }
    info.size = dl.size;
    if (!e.sha256.empty()) {
        if (dl.sha != e.sha256) {
            DeleteFileA(cache.c_str());
            *msg = "checksum mismatch for '" + file + "' (index says " + e.sha256.substr(0, 12) + "..., got " +
                   (dl.sha.empty() ? std::string("?") : dl.sha.substr(0, 12) + "...") + ") - the download was deleted.";
            return info;
        }
    } else if (type == "program" || type == "archive") {
        DeleteFileA(cache.c_str());
        *msg = "refusing to install '" + e.name + "': the repo gives no sha256 for a " + type + " package.";
        return info;
    }

    // -- put it in place
    info.dest = dest;
    if (type == "plugin" || type == "file") {
        std::string dir = (type == "plugin") ? PluginsDir() : dest_dir;
        if (!CreateDirRecursive(dir)) { DeleteFileA(cache.c_str()); *msg = "couldn't create '" + dir + "'."; return info; }
        if (!MoveFileExA(cache.c_str(), dest.c_str(), MOVEFILE_REPLACE_EXISTING | MOVEFILE_COPY_ALLOWED)) {
            DeleteFileA(cache.c_str());
            *msg = "couldn't write '" + dest + "' (error " + std::to_string(GetLastError()) + ").";
            return info;
        }
    } else if (type == "archive") {
        if (!CreateDirRecursive(dest_dir)) { DeleteFileA(cache.c_str()); *msg = "couldn't create '" + dest_dir + "'."; return info; }
        std::string err;
        bool ok = ExtractZip(cache, dest_dir, &err);
        DeleteFileA(cache.c_str());
        if (!ok) { *msg = err; return info; }
    } else { // program
        bool do_run = o.run;
        if (!do_run && o.interactive) do_run = ConfirmRun("Run the installer now?");
        if (!do_run) {
            info.ok = true;
            info.note = "saved to " + cache + " - run it yourself, or: hom install " + e.name + " --run";
            return info; // not installed yet, so nothing goes into the database
        }
        DWORD code = 0;
        if (!RunProgram(cache, e.args, &code)) { *msg = "couldn't start '" + cache + "'."; return info; }
        info.ran = true;
        info.note = "the installer exited with code " + std::to_string(code);
        if (code != 0) { *msg = info.note + " - not recorded as installed."; return info; }
    }

    DbPut({e.name, type, e.version, dest});
    info.ok = true;
    return info;
}

static const IndexEntry *FindEntry(const std::vector<IndexEntry> &idx, const std::string &name) {
    for (const auto &e : idx) if (e.name == name) return &e;
    return nullptr;
}

// Non-plugin packages (file/archive/program): deletes what hom put on disk. Returns false when
// `name` isn't one of those (the caller then treats it as a plugin).
static bool RemoveDbPkg(const std::string &name, std::string *msg, int *rc) {
    std::vector<homcommon::DbRec> db = DbLoad();
    const homcommon::DbRec *r = DbFind(db, name);
    if (!r || r->type == "plugin") return false;
    homcommon::DbRec rec = *r;
    *rc = 0;
    if (rec.type == "file") {
        DeleteFileA(rec.path.c_str());
        size_t cut = rec.path.find_last_of('\\');
        // Only hom's own per-package folder goes; a folder chosen with --to stays.
        if (cut != std::string::npos && IsUnder(rec.path.substr(0, cut), g_data + "\\files"))
            RemoveDirectoryA(rec.path.substr(0, cut).c_str());
        *msg = "Removed " + rec.path;
    } else if (rec.type == "archive") {
        if (IsUnder(rec.path, g_data + "\\apps")) {
            if (!DeleteDirRecursive(rec.path)) { *msg = "couldn't fully remove '" + rec.path + "'."; *rc = 1; return true; }
            *msg = "Removed " + rec.path;
        } else {
            *msg = "'" + name + "' was unpacked into " + rec.path + ", outside hom's data folder - hom leaves that folder alone, "
                   "delete it yourself if you want it gone. Forgot the package.";
        }
    } else { // program
        DeleteFileA(rec.path.c_str());
        *msg = "Forgot '" + name + "' and removed its saved installer. To uninstall the program itself use Windows' Installed apps.";
    }
    DbDel(name);
    return true;
}

// -----------------------------------------------------------------------
// Commands
// -----------------------------------------------------------------------

// `hom ping` -- a quick "is the package repo reachable and responding" check, the same way
// `ping` on a shell checks basic connectivity, but against the one thing hom depends on.
int CmdPing() {
    std::printf("Pinging %s (repo '%s')...\n", g_repo_url.c_str(), g_repo_name.c_str());
    LARGE_INTEGER freq, t0, t1;
    QueryPerformanceFrequency(&freq);
    QueryPerformanceCounter(&t0);

    bool idx_ok = false;
    std::vector<IndexEntry> idx = FetchIndex(&idx_ok);
    FetchResult res;
    if (!idx_ok) res = FetchFromRepo(L"index.json"); // only to tell "no network" from "no index"

    QueryPerformanceCounter(&t1);
    double ms = (double)(t1.QuadPart - t0.QuadPart) * 1000.0 / (double)freq.QuadPart;

    if (idx_ok) {
        std::printf("hom: OK -- repo responded in %.0f ms (%zu package(s) in the index)\n", ms, idx.size());
        return 0;
    }
    if (!res.ok) {
        std::fprintf(stderr, "hom: no response (%.0f ms) -- check your connection.\n", ms);
        return 1;
    }
    std::printf("hom: reached the host but got HTTP %d for index.json (%.0f ms) -- repo may be misconfigured or moved.\n",
                res.status, ms);
    return 1;
}

// `hom update` -- compares version.txt of the latest hom release with this binary; if it's newer,
// downloads hom.exe from that release, checks its sha256 (hom.exe.sha256 next to it) and swaps it in
// for the running one. Always the official hom repo, whatever package repo is selected.
int CmdUpdate(bool force) {
    std::printf("Checking %s for updates...\n", homcommon::kSelfVerUrl);

    FetchResult vres = FetchUrl(Widen(homcommon::kSelfVerUrl));
    if (!vres.ok || vres.status != 200) {
        std::fprintf(stderr, "hom: couldn't reach the repo (status %d). Check your connection and try again.\n", vres.status);
        return 1;
    }

    std::string latest = Trim(vres.body);
    if (latest.empty()) {
        std::fprintf(stderr, "hom: repo returned an empty version -- can't tell if an update is needed.\n");
        return 1;
    }

    if (!VersionGt(latest, k_hom_version) && !force) {
        std::printf("hom is already up to date (%s).\n", k_hom_version);
        return 0;
    }
    if (!VersionGt(latest, k_hom_version) && force) {
        std::printf("hom is already up to date (%s), but -f/--force was given -- reinstalling anyway.\n", k_hom_version);
    } else {
        std::printf("New hom version available: %s -> %s\n", k_hom_version, latest.c_str());
    }
    std::printf("Downloading hom.exe...\n");

    ProgressReporter prog(g_progress_mode);
    FetchResult bres = FetchUrl(Widen(homcommon::kSelfExeUrl), L"GET", &prog);
    prog.Finish();
    if (!bres.ok || bres.status != 200 || bres.body.size() < 2 || bres.body[0] != 'M' || bres.body[1] != 'Z') {
        std::fprintf(stderr, "hom: download failed (status %d). Update aborted, nothing changed.\n", bres.status);
        return 1;
    }

    FetchResult sres = FetchUrl(Widen(homcommon::kSelfShaUrl));
    if (sres.ok && sres.status == 200 && sres.body.size() >= 64) {
        std::string want = Lower(sres.body.substr(0, 64));
        std::string got = Sha256Of(bres.body);
        if (want != got) {
            std::fprintf(stderr, "hom: checksum mismatch (release says %s..., downloaded file is %s...). Update aborted, nothing changed.\n",
                         want.substr(0, 12).c_str(), got.substr(0, 12).c_str());
            return 1;
        }
        std::printf("Checksum OK (sha256 %s...).\n", got.substr(0, 12).c_str());
    } else {
        std::printf("hom: this release has no hom.exe.sha256 -- skipping the checksum check.\n");
    }

    std::string dir      = ExeDir();
    std::string self     = ExePath();
    std::string new_path = dir + "\\hom.exe.new";
    std::string old_path = dir + "\\hom.exe.old";

    if (!WriteFileBytes(new_path, bres.body)) {
        std::fprintf(stderr, "hom: couldn't write '%s'. Check disk space/permissions.\n", new_path.c_str());
        return 1;
    }

    // Best-effort: clear out a stale .old from a previous update before we create a new one.
    DeleteFileA(old_path.c_str());

    if (!MoveFileExA(self.c_str(), old_path.c_str(), MOVEFILE_REPLACE_EXISTING)) {
        std::fprintf(stderr, "hom: couldn't move the running exe out of the way (error %lu). Update aborted.\n", GetLastError());
        DeleteFileA(new_path.c_str());
        return 1;
    }
    if (!MoveFileExA(new_path.c_str(), self.c_str(), MOVEFILE_REPLACE_EXISTING)) {
        std::fprintf(stderr, "hom: couldn't move the new build into place (error %lu). "
                              "Your old hom.exe is safe at '%s' -- rename it back manually.\n",
                     GetLastError(), old_path.c_str());
        return 1;
    }

    std::printf("Updated to %s. Old binary kept at 'hom.exe.old' (safe to delete).\n", latest.c_str());
    return 0;
}

// Prints the result of an InstallPkg() call the way the command line shows it.
static int ReportInstall(const IndexEntry &e, const InstallInfo &r, const std::string &msg, bool guessed) {
    if (r.cancelled) { std::printf("hom: %s\n", msg.c_str()); return 1; }
    if (!r.ok) {
        if (guessed && r.status == 404)
            std::fprintf(stderr, "hom: no package named '%s' in the repo.\n", e.name.c_str());
        else
            std::fprintf(stderr, "hom: %s\n", msg.c_str());
        return 1;
    }
    if (r.type == "program") {
        if (r.ran) std::printf("Ran the installer for '%s' (%lld bytes). %s.\n", e.name.c_str(), r.size, r.note.c_str());
        else       std::printf("Downloaded '%s' (%lld bytes) and checked its sha256: %s\n", e.name.c_str(), r.size, r.note.c_str());
        return 0;
    }
    std::printf("%s '%s' -> %s (%lld bytes)\n", r.existed ? "Updated" : "Installed", e.name.c_str(), r.dest.c_str(), r.size);
    if (r.type == "plugin") std::printf("Restart HomRec (or reload plugins) to pick it up.\n");
    return 0;
}

// `hom install <n>` -- looks <n> up in the repo's index and installs it by its type.
int CmdInstall(const std::string &name, const Flags &f, const std::vector<IndexEntry> *idx_in = nullptr) {
    if (name.empty()) {
        std::fprintf(stderr, "hom: install needs a package name, e.g. 'hom install input-overlay'\n");
        return 1;
    }
    std::vector<IndexEntry> idx;
    if (!idx_in) { bool ok; idx = FetchIndex(&ok); idx_in = &idx; }

    // A name that isn't in the index is still tried as <repo>/<name>.hrp, like hom always did.
    IndexEntry guess;
    const IndexEntry *e = FindEntry(*idx_in, name);
    bool guessed = !e;
    if (guessed) { guess.name = name; guess.file = name + ".hrp"; e = &guess; }

    std::printf("Fetching %s '%s'...\n", PkgType(*e).c_str(), name.c_str());
    InstallOpts o;
    o.yes = f.yes; o.force = f.force; o.run = f.run; o.interactive = true; o.to = f.to;
    ProgressReporter prog(g_progress_mode);
    std::string msg;
    InstallInfo r = InstallPkg(*e, o, &prog, &msg);
    prog.Finish();
    return ReportInstall(*e, r, msg, guessed);
}

int CmdUpgrade(bool yes, bool force) {
    bool idx_ok = false;
    std::vector<IndexEntry> idx = FetchIndex(&idx_ok);

    // Plugins: every installed one is fetched again (as before). Other packages: only when the
    // repo has a newer version than the one hom recorded.
    std::vector<std::string> names;
    std::map<std::string, std::string> jar_names;
    for (const auto &p : ListLocalPlugins()) {
        if (p.kind == "hrp" || p.kind == "jar") {
            names.push_back(p.name);
            if (p.kind == "jar") jar_names[p.name] = p.name + ".jar";
        } else if (p.kind == "file" || p.kind == "archive" || p.kind == "program") {
            const IndexEntry *e = FindEntry(idx, p.name);
            if (e && !p.version.empty() && VersionGt(e->version, p.version)) names.push_back(p.name);
        }
    }

    if (names.empty()) {
        std::printf("hom: no installed packages found -- nothing to upgrade.\n");
        return 0;
    }

    std::printf("Upgrading %zu installed package(s)...\n", names.size());
    size_t failures = 0;
    for (const auto &name : names) {
        // Upgrade is inherently "-y": it shouldn't stop and ask once per package.
        Flags f;
        f.yes = true;
        f.force = force;
        std::vector<IndexEntry> one = idx;
        if (!FindEntry(one, name) && jar_names.count(name)) {
            IndexEntry g; g.name = name; g.file = jar_names[name];
            one.push_back(g);
        }
        if (CmdInstall(name, f, &one) != 0) ++failures;
    }
    (void)yes;
    std::printf("Done: %zu upgraded, %zu failed.\n", names.size() - failures, failures);
    return failures ? 1 : 0;
}

// The older name for the same operation -- also what `hom install update-hrp` still dispatches to.
int CmdUpdateAllPlugins() { return CmdUpgrade(/*yes=*/true, /*force=*/false); }

// `hom full-upgrade` -- see the header comment at the top of this file:
int CmdFullUpgrade(bool yes, bool force) {
    int rc = CmdUpgrade(yes, force);
    std::printf("hom: full-upgrade currently behaves the same as 'hom upgrade' -- "
                "packages have no dependencies between them yet for it to resolve by removing anything.\n");
    return rc;
}

// `hom autoremove` -- hom doesn't record which packages (if any, in the future) were pulled in only
// as a dependency of something else, so there's nothing it could safely identify as "orphaned" today.
int CmdAutoremove() {
    std::printf("hom: nothing to do -- hom installs each package on its own "
                "and doesn't track dependency-installed packages, so there are no orphans to find yet.\n");
    return 0;
}

struct FoundPluginPaths {
    std::string hrp_path;      // the .hrp, or the .jar when that is what's installed
    std::string installed_dir;
    std::string plain_dir;
    bool has_hrp = false, has_installed = false, has_plain = false;
};
FoundPluginPaths LocatePluginPaths(const std::string &name) {
    FoundPluginPaths p;
    if (g_homrec.empty()) return p; // never build a path from an empty plugins folder
    p.hrp_path      = PluginsDir() + "\\" + name + ".hrp";
    p.installed_dir = PluginsDir() + "\\.installed\\" + name;
    p.plain_dir     = PluginsDir() + "\\" + name; // plugins shipped as a bare folder, not a .hrp
    p.has_hrp       = FileExistsA(p.hrp_path);
    if (!p.has_hrp && FileExistsA(PluginsDir() + "\\" + name + ".jar")) {
        p.hrp_path = PluginsDir() + "\\" + name + ".jar";
        p.has_hrp = true;
    }
    p.has_installed = DirExistsA(p.installed_dir);
    p.has_plain     = DirExistsA(p.plain_dir);
    return p;
}

// Common argument validation for remove/purge: name present, name safe, -r given.
// `verb` is folded into the messages ("remove"/"purge").
int ValidateRemovalArgs(const std::string &name, bool rflag, const char *verb) {
    if (name.empty()) {
        std::fprintf(stderr, "hom: %s needs a package name, e.g. 'hom %s input-overlay -r'\n", verb, verb);
        return 1;
    }
    std::string reason;
    if (!IsSafePluginName(name, &reason)) {
        std::fprintf(stderr, "hom: refusing package name '%s' (%s) -- this could resolve outside hom's folders.\n",
                     name.c_str(), reason.c_str());
        return 1;
    }
    if (!rflag) {
        std::fprintf(stderr, "hom: %s needs -r to confirm deletion, e.g. 'hom %s %s -r'\n",
                     verb, verb, name.c_str());
        return 1;
    }
    return 0;
}

int CmdRemove(const std::string &name, bool rflag) {
    if (int rc = ValidateRemovalArgs(name, rflag, "remove")) return rc;
    std::string m; int rc = 0;
    if (RemoveDbPkg(name, &m, &rc)) { std::printf("%s\n", m.c_str()); return rc; }
    FoundPluginPaths p = LocatePluginPaths(name);

    if (!p.has_hrp && !p.has_installed && !p.has_plain) {
        std::fprintf(stderr, "hom: no package named '%s' is installed here.%s\n", name.c_str(),
                     g_homrec.empty() ? " (No HomRec folder found, so plugins weren't checked -- see 'hom where'.)" : "");
        return 1;
    }

    if (p.has_hrp) {
        if (!DeleteFileA(p.hrp_path.c_str())) {
            std::fprintf(stderr, "hom: couldn't delete '%s' (error %lu).\n", p.hrp_path.c_str(), GetLastError());
            return 1;
        }
        std::printf("Removed %s\n", p.hrp_path.c_str());
    }
    for (const std::string &dir : { p.installed_dir, p.plain_dir }) {
        if (!DirExistsA(dir)) continue;
        bool had_store = FileExistsA(dir + "\\.store");
        if (!DeleteDirContentsExcept(dir, ".store")) {
            std::fprintf(stderr, "hom: couldn't fully clean up '%s'.\n", dir.c_str());
            return 1;
        }
        std::printf(had_store ? "Removed %s (kept its .store)\n" : "Removed %s\n", dir.c_str());
    }
    DbDel(name);
    std::printf("hom: plugin removed. Its saved settings (if any) were kept -- "
                "use 'hom purge %s -r' to delete those too.\n", name.c_str());
    return 0;
}

// `hom purge <n> -r` -- same as `remove`, but also deletes .store, so nothing of the plugin is
// left behind. -r is required, same reasoning as remove.
int CmdPurge(const std::string &name, bool rflag) {
    if (int rc = ValidateRemovalArgs(name, rflag, "purge")) return rc;
    std::string m; int rc = 0;
    if (RemoveDbPkg(name, &m, &rc)) { std::printf("%s\n", m.c_str()); return rc; }
    FoundPluginPaths p = LocatePluginPaths(name);

    if (!p.has_hrp && !p.has_installed && !p.has_plain) {
        std::fprintf(stderr, "hom: no package named '%s' is installed here.%s\n", name.c_str(),
                     g_homrec.empty() ? " (No HomRec folder found, so plugins weren't checked -- see 'hom where'.)" : "");
        return 1;
    }

    if (p.has_hrp) {
        if (!DeleteFileA(p.hrp_path.c_str())) {
            std::fprintf(stderr, "hom: couldn't delete '%s' (error %lu).\n", p.hrp_path.c_str(), GetLastError());
            return 1;
        }
        std::printf("Removed %s\n", p.hrp_path.c_str());
    }
    for (const std::string &dir : { p.installed_dir, p.plain_dir }) {
        if (!DirExistsA(dir)) continue;
        if (!DeleteDirRecursive(dir)) {
            std::fprintf(stderr, "hom: couldn't fully clean up '%s'.\n", dir.c_str());
            return 1;
        }
        std::printf("Removed %s\n", dir.c_str());
    }
    DbDel(name);
    return 0;
}

// `hom search <query>` -- matches `query` against each index entry's name + description.
int CmdSearch(const std::string &query) {
    if (query.empty()) {
        std::fprintf(stderr, "hom: search needs a query, e.g. 'hom search overlay'\n");
        return 1;
    }
    bool ok;
    std::vector<IndexEntry> entries = FetchIndex(&ok);
    if (!ok) {
        std::fprintf(stderr, "hom: couldn't fetch the package index -- check your connection.\n");
        return 1;
    }

    std::string lower_query = Lower(query);

    std::regex re;
    bool have_regex = true;
    try {
        re = std::regex(query, std::regex::ECMAScript | std::regex::icase);
    } catch (const std::regex_error &) {
        have_regex = false;
    }

    auto matches = [&](const std::string &haystack) {
        if (have_regex && std::regex_search(haystack, re)) return true;
        return Lower(haystack).find(lower_query) != std::string::npos;
    };

    int found = 0;
    for (const auto &e : entries) {
        if (!matches(e.name) && !matches(e.description)) continue;
        ++found;
        std::string type = PkgType(e);
        std::printf("%s (%s)%s - %s\n", e.name.c_str(),
                    e.version.empty() ? "?" : e.version.c_str(),
                    type == "plugin" ? "" : (" [" + type + "]").c_str(),
                    e.description.empty() ? "(no description)" : e.description.c_str());
    }
    if (!found) std::printf("hom: no packages matching '%s'.\n", query.c_str());
    return 0;
}

// `hom show <n>` -- prints what the repo's index knows about a package, plus its local status.
int CmdShow(const std::string &name) {
    if (name.empty()) {
        std::fprintf(stderr, "hom: show needs a package name, e.g. 'hom show input-overlay'\n");
        return 1;
    }
    bool ok;
    std::vector<IndexEntry> entries = FetchIndex(&ok);
    if (!ok) {
        std::fprintf(stderr, "hom: couldn't fetch the package index -- check your connection.\n");
        return 1;
    }

    const IndexEntry *found = FindEntry(entries, name);
    if (!found) {
        std::fprintf(stderr, "hom: no package named '%s' in the repo index.\n", name.c_str());
        return 1;
    }

    std::printf("Name:        %s\n", found->name.c_str());
    std::printf("Type:        %s\n", PkgType(*found).c_str());
    std::printf("Version:     %s\n", found->version.empty() ? "?" : found->version.c_str());
    if (!found->author.empty()) std::printf("Author:      %s\n", found->author.c_str());
    std::printf("Description: %s\n", found->description.empty() ? "(none)" : found->description.c_str());
    std::string file = found->file.empty() ? (name + ".hrp") : found->file;
    std::string err;
    std::wstring url = PkgUrl(*found, &err);
    std::printf("Package:     %s\n", found->url.empty() ? file.c_str() : found->url.c_str());
    if (!found->sha256.empty()) std::printf("SHA-256:     %s\n", found->sha256.c_str());

    if (!url.empty()) {
        FetchResult head = FetchUrl(url, L"HEAD");
        if (head.ok && head.status == 200 && head.content_length >= 0)
            std::printf("Size:        %s\n", homcommon::FormatBytes(head.content_length).c_str());
    }

    for (const auto &p : ListLocalPlugins()) {
        if (p.name != name) continue;
        std::printf("Installed:   yes (%s)\n", p.version.empty() ? "version unknown -- not yet loaded by HomRec" : p.version.c_str());
        return 0;
    }
    std::printf("Installed:   no\n");
    return 0;
}

// `hom list --installed` / `hom list --upgradable`.
int CmdList(bool upgradable_only) {
    std::vector<LocalPlugin> local = ListLocalPlugins();
    if (local.empty()) {
        std::printf(g_homrec.empty() ? "hom: nothing installed (no HomRec folder found for plugins, see 'hom where').\n"
                                     : "hom: nothing installed.\n");
        return 0;
    }

    if (!upgradable_only) {
        for (const auto &p : local) {
            std::string tag = p.kind == "folder" ? " [folder]" : (p.kind == "hrp" ? "" : " [" + p.kind + "]");
            std::printf("%s (%s)%s\n", p.name.c_str(),
                        p.version.empty() ? "version unknown" : p.version.c_str(), tag.c_str());
        }
        return 0;
    }

    bool ok;
    std::vector<IndexEntry> remote = FetchIndex(&ok);
    if (!ok) {
        std::fprintf(stderr, "hom: couldn't fetch the package index -- check your connection.\n");
        return 1;
    }

    int upgradable = 0, unknown = 0;
    for (const auto &p : local) {
        const IndexEntry *r = FindEntry(remote, p.name);
        if (!r) continue; // not in the repo (anymore, or a local-only package) -- nothing hom can offer
        if (p.version.empty()) {
            ++unknown;
            continue;
        }
        if (VersionGt(r->version, p.version)) {
            ++upgradable;
            std::printf("%s: %s -> %s\n", p.name.c_str(), p.version.c_str(), r->version.c_str());
        }
    }
    if (!upgradable) std::printf("hom: no upgrades available.\n");
    if (unknown) {
        std::printf("hom: %d package(s) skipped -- local version unknown (not yet loaded by HomRec since install; "
                    "run HomRec once, or 'hom upgrade', to find out).\n", unknown);
    }
    return 0;
}

// `hom where` -- everything hom worked out about its surroundings, for "why did it install there?".
int CmdWhere() {
    std::printf("hom:       %s (%s)\n", ExePath().c_str(), k_hom_version);
    std::printf("settings:  %s\n", g_state.c_str());
    std::printf("data:      %s\n", g_data.c_str());
    if (g_homrec.empty()) std::printf("HomRec:    not found (only plugin packages need it; 'hom set homrec <folder>')\n");
    else                  std::printf("HomRec:    %s\n", g_homrec.c_str());
    std::printf("repo:      %s  %s\n", g_repo_name.c_str(), g_repo_url.c_str());
    return 0;
}

// `hom set <key> [value]` -- homrec / data. 'auto' forgets the setting.
int CmdSet(const std::string &key, const std::string &value, bool has_value) {
    if (key != "homrec" && key != "data") {
        std::fprintf(stderr, "hom: unknown setting '%s' (homrec, data).\n", key.c_str());
        return 1;
    }
    std::filesystem::path file = std::filesystem::path(g_state) / homcommon::kCfgName;
    std::map<std::string, std::string> kv = homcommon::LoadKv(file);
    if (!has_value) {
        std::printf("%s = %s\n", key.c_str(), kv[key].empty() ? "(auto)" : kv[key].c_str());
        return 0;
    }
    if (value == "auto" || value == "-") {
        kv.erase(key);
    } else {
        char full[MAX_PATH];
        DWORD n = GetFullPathNameA(value.c_str(), MAX_PATH, full, nullptr);
        if (n == 0 || n >= MAX_PATH) { std::fprintf(stderr, "hom: bad folder '%s'.\n", value.c_str()); return 1; }
        std::string dir = full;
        while (dir.size() > 3 && (dir.back() == '\\' || dir.back() == '/')) dir.pop_back();
        if (key == "homrec" && !LooksLikeHomrec(dir))
            std::fprintf(stderr, "hom: warning: no hr.exe or homrec.exe in '%s' -- saving it anyway.\n", dir.c_str());
        kv[key] = dir;
    }
    if (!homcommon::SaveKv(file, kv)) {
        std::fprintf(stderr, "hom: couldn't write %s.\n", file.string().c_str());
        return 1;
    }
    std::printf("Saved. %s\n", kv.count(key) ? (key + " = " + kv[key]).c_str() : (key + " is automatic again").c_str());
    return 0;
}

// `hom clean` -- installers and archives stay in <data>\dl after a download; this empties it.
int CmdClean() {
    std::string dl = g_data + "\\dl";
    if (!DirExistsA(dl)) { std::printf("hom: nothing to clean.\n"); return 0; }
    if (!DeleteDirRecursive(dl)) { std::fprintf(stderr, "hom: couldn't fully clean '%s'.\n", dl.c_str()); return 1; }
    std::printf("Cleaned %s\n", dl.c_str());
    return 0;
}

// -----------------------------------------------------------------------
// Repositories (`hom repo ...`)
// -----------------------------------------------------------------------

// hom_repos.cfg lives in hom's settings folder (g_state, see InitPaths()), not in the cwd: hr.exe's
// console runs hom with cwd = HomRec's folder, but a hom.exe run from somewhere else must still find
// the same repo list.
static std::filesystem::path RepoCfgPath() {
    return homcommon::RepoCfgFile(std::filesystem::path(g_state));
}

// Points every FetchFromRepo() at `url` (already normalized https URL).
static bool ApplyRepoUrl(const std::string &name, const std::string &url) {
    std::wstring wurl = Widen(url);
    URL_COMPONENTS uc = {};
    uc.dwStructSize = sizeof(uc);
    wchar_t host[256] = {}, path[2048] = {};
    uc.lpszHostName = host; uc.dwHostNameLength = _countof(host);
    uc.lpszUrlPath  = path; uc.dwUrlPathLength  = _countof(path);
    if (!WinHttpCrackUrl(wurl.c_str(), (DWORD)wurl.size(), 0, &uc)) return false;
    if (uc.nScheme != INTERNET_SCHEME_HTTPS) return false;
    if (wurl.back() != L'/') wurl.push_back(L'/');
    g_repo_base = wurl;
    g_repo_name = name;
    g_repo_url  = url;
    return true;
}

// Called once from main() before any network command.
static void SelectActiveRepoFromConfig() {
    homcommon::RepoConfig cfg = homcommon::LoadRepoConfig(RepoCfgPath());
    homcommon::Repo active = homcommon::ActiveRepo(cfg);
    if (!ApplyRepoUrl(active.name, active.url)) {
        std::fprintf(stderr, "hom: repo '%s' has an unusable URL (%s) -- falling back to the official repo.\n",
                     active.name.c_str(), active.url.c_str());
        ApplyRepoUrl(homcommon::kOfficialName, homcommon::kOfficialUrl);
    }
}

int CmdRepoList() {
    homcommon::RepoConfig cfg = homcommon::LoadRepoConfig(RepoCfgPath());
    homcommon::Repo active = homcommon::ActiveRepo(cfg);
    bool official_active = homcommon::EqualsNoCase(active.name, homcommon::kOfficialName);

    std::printf("%c %-14s %s\n", official_active ? '*' : ' ', homcommon::kOfficialName, homcommon::kOfficialUrl);
    for (const auto &r : cfg.repos) {
        bool is_active = !official_active && homcommon::EqualsNoCase(r.name, active.name);
        std::printf("%c %-14s %s\n", is_active ? '*' : ' ', r.name.c_str(), r.url.c_str());
    }
    if (!homcommon::EqualsNoCase(cfg.active, homcommon::kOfficialName) && !homcommon::FindRepo(cfg, cfg.active)) {
        std::printf("hom: the saved active repo '%s' no longer exists -- using official.\n", cfg.active.c_str());
    }
    return 0;
}

int CmdRepoAdd(const std::string &name, const std::string &url_in) {
    if (name.empty() || url_in.empty()) {
        std::fprintf(stderr, "hom: usage: hom repo add <name> <https-url | owner/repo[@branch]>\n"
                              "     e.g.  hom repo add myfork someone/hom\n"
                              "           hom repo add mirror https://example.org/hom/pkgs/\n");
        return 1;
    }
    std::string why;
    if (!homcommon::IsValidRepoName(name, &why)) {
        std::fprintf(stderr, "hom: bad repo name '%s' (%s).\n", name.c_str(), why.c_str());
        return 1;
    }
    std::string url;
    if (!homcommon::NormalizeRepoUrl(url_in, &url, &why)) {
        std::fprintf(stderr, "hom: bad repo URL (%s).\n", why.c_str());
        return 1;
    }
    homcommon::RepoConfig cfg = homcommon::LoadRepoConfig(RepoCfgPath());
    if (homcommon::FindRepo(cfg, name)) {
        std::fprintf(stderr, "hom: a repo named '%s' already exists (hom repo remove %s, or pick another name).\n",
                     name.c_str(), name.c_str());
        return 1;
    }
    cfg.repos.push_back({name, url});
    if (!homcommon::SaveRepoConfig(RepoCfgPath(), cfg)) {
        std::fprintf(stderr, "hom: couldn't write %s. Check folder permissions.\n", RepoCfgPath().string().c_str());
        return 1;
    }
    std::printf("Added repo '%s' -> %s\n", name.c_str(), url.c_str());
    std::printf("Use 'hom repo set %s' to download from it.\n", name.c_str());
    return 0;
}

int CmdRepoSet(const std::string &name) {
    if (name.empty()) {
        std::fprintf(stderr, "hom: usage: hom repo set <name>   (see 'hom repo list')\n");
        return 1;
    }
    homcommon::RepoConfig cfg = homcommon::LoadRepoConfig(RepoCfgPath());
    std::string target;
    std::string url;
    if (homcommon::EqualsNoCase(name, homcommon::kOfficialName)) {
        target = homcommon::kOfficialName;
        url = homcommon::kOfficialUrl;
    } else if (const homcommon::Repo *r = homcommon::FindRepo(cfg, name)) {
        target = r->name;
        url = r->url;
    } else {
        std::fprintf(stderr, "hom: no repo named '%s' (hom repo list; add it with hom repo add).\n", name.c_str());
        return 1;
    }
    cfg.active = target;
    if (!homcommon::SaveRepoConfig(RepoCfgPath(), cfg)) {
        std::fprintf(stderr, "hom: couldn't write %s. Check folder permissions.\n", RepoCfgPath().string().c_str());
        return 1;
    }
    std::printf("Now using repo '%s' (%s)\n", target.c_str(), url.c_str());
    std::printf("Installed packages stay as they are; install/upgrade now download from this repo (hom update always uses the official hom repo).\n");
    return 0;
}

int CmdRepoRemove(const std::string &name) {
    if (name.empty()) {
        std::fprintf(stderr, "hom: usage: hom repo remove <name>\n");
        return 1;
    }
    if (homcommon::EqualsNoCase(name, homcommon::kOfficialName)) {
        std::fprintf(stderr, "hom: the official repo is built in and can't be removed.\n");
        return 1;
    }
    homcommon::RepoConfig cfg = homcommon::LoadRepoConfig(RepoCfgPath());
    auto it = std::find_if(cfg.repos.begin(), cfg.repos.end(),
                           [&](const homcommon::Repo &r) { return homcommon::EqualsNoCase(r.name, name); });
    if (it == cfg.repos.end()) {
        std::fprintf(stderr, "hom: no repo named '%s'.\n", name.c_str());
        return 1;
    }
    std::string removed = it->name;
    cfg.repos.erase(it);
    bool was_active = homcommon::EqualsNoCase(cfg.active, removed);
    if (was_active) cfg.active = homcommon::kOfficialName;
    if (!homcommon::SaveRepoConfig(RepoCfgPath(), cfg)) {
        std::fprintf(stderr, "hom: couldn't write %s. Check folder permissions.\n", RepoCfgPath().string().c_str());
        return 1;
    }
    std::printf("Removed repo '%s'.\n", removed.c_str());
    if (was_active) std::printf("It was the one in use -- switched back to 'official'.\n");
    return 0;
}

// -----------------------------------------------------------------------
// Interactive TUI (`hom tui`, or plain `hom` in a real console)
//
// A small full-screen package browser in the spirit of `pacman`/`aptitude`:
// the repo index and the local plugins folder are merged into one list,
// Up/Down selects, i installs/upgrades, r/p remove/purge, u upgrades all,
// / filters, Tab switches the view. All drawing goes through the Win32
// console API (WriteConsoleOutputW), so it needs a real console and is
// deliberately unavailable when stdout is a pipe (e.g. HomRec's in-app
// console) -- plain commands keep working there exactly as before.
// -----------------------------------------------------------------------

#ifdef _WIN32
namespace tui {

constexpr WORD kAttrText    = FOREGROUND_RED | FOREGROUND_GREEN | FOREGROUND_BLUE;
constexpr WORD kAttrDim     = FOREGROUND_INTENSITY;
constexpr WORD kAttrBold    = FOREGROUND_RED | FOREGROUND_GREEN | FOREGROUND_BLUE | FOREGROUND_INTENSITY;
constexpr WORD kAttrGreen   = FOREGROUND_GREEN | FOREGROUND_INTENSITY;
constexpr WORD kAttrYellow  = FOREGROUND_RED | FOREGROUND_GREEN | FOREGROUND_INTENSITY;
constexpr WORD kAttrCyan    = FOREGROUND_GREEN | FOREGROUND_BLUE | FOREGROUND_INTENSITY;
constexpr WORD kAttrRed     = FOREGROUND_RED | FOREGROUND_INTENSITY;
constexpr WORD kAttrHeader  = BACKGROUND_BLUE | FOREGROUND_RED | FOREGROUND_GREEN | FOREGROUND_BLUE | FOREGROUND_INTENSITY;
constexpr WORD kAttrSel     = BACKGROUND_BLUE | BACKGROUND_GREEN | FOREGROUND_RED | FOREGROUND_GREEN | FOREGROUND_BLUE;
constexpr WORD kAttrBox     = FOREGROUND_BLUE | FOREGROUND_GREEN | FOREGROUND_INTENSITY;
constexpr WORD kAttrDialog  = BACKGROUND_BLUE | FOREGROUND_RED | FOREGROUND_GREEN | FOREGROUND_BLUE | FOREGROUND_INTENSITY;

// True when the process' own std handles are consoles. Used for the
// "bare `hom` opens the TUI" shortcut: with redirected output (a pipe or a
// file) we must not take over a screen the caller isn't looking at.
bool IsConsole() {
    DWORD mode = 0;
    return GetConsoleMode(GetStdHandle(STD_INPUT_HANDLE), &mode) &&
           GetConsoleMode(GetStdHandle(STD_OUTPUT_HANDLE), &mode) != 0;
}

// -- Small text helpers ------------------------------------------------------

std::wstring Fit(const std::wstring &s, int w) {
    if (w <= 0) return {};
    if ((int)s.size() <= w) return s;
    if (w <= 3) return s.substr(0, (size_t)w);
    return s.substr(0, (size_t)(w - 3)) + L"...";
}

std::wstring PadLeft(const std::wstring &s, int w) {
    if ((int)s.size() >= w) return Fit(s, w);
    return std::wstring((size_t)(w - (int)s.size()), L' ') + s;
}

std::vector<std::wstring> Wrap(const std::wstring &text, int width) {
    std::vector<std::wstring> lines;
    if (width <= 0) return lines;
    size_t i = 0;
    while (i < text.size()) {
        while (i < text.size() && text[i] == L' ') ++i;
        if (i >= text.size()) break;
        size_t end = i;
        size_t last_space = std::wstring::npos;
        while (end < text.size() && (int)(end - i) < width) {
            if (text[end] == L' ') last_space = end;
            ++end;
        }
        if (end < text.size() && text[end] != L' ' && last_space != std::wstring::npos && last_space > i)
            end = last_space;
        lines.push_back(text.substr(i, end - i));
        i = end;
        if (i < text.size() && text[i] == L' ') ++i;
    }
    if (lines.empty()) lines.push_back(L"");
    return lines;
}

std::string LowerAscii(std::string s) {
    for (char &c : s) c = (char)std::tolower((unsigned char)c);
    return s;
}

// -- Console screen ----------------------------------------------------------

class Screen {
public:
    void Attach(HANDLE out) {
        out_ = out;
        w_ = h_ = 0;
        Resize();
    }

    void Resize() {
        if (!out_) return;
        CONSOLE_SCREEN_BUFFER_INFO csbi{};
        if (!GetConsoleScreenBufferInfo(out_, &csbi)) return;
        int nw = csbi.srWindow.Right - csbi.srWindow.Left + 1;
        int nh = csbi.srWindow.Bottom - csbi.srWindow.Top + 1;
        if (nw == w_ && nh == h_) return;
        w_ = nw;
        h_ = nh;
        cells_.assign((size_t)w_ * (size_t)h_, CHAR_INFO{});
    }

    int width() const { return w_; }
    int height() const { return h_; }

    void Clear(WORD attr = kAttrText) {
        for (auto &c : cells_) { c.Char.UnicodeChar = L' '; c.Attributes = attr; }
    }

    void Put(int x, int y, wchar_t ch, WORD attr) {
        if (x < 0 || y < 0 || x >= w_ || y >= h_) return;
        CHAR_INFO &c = cells_[(size_t)y * (size_t)w_ + (size_t)x];
        c.Char.UnicodeChar = ch;
        c.Attributes = attr;
    }

    void Text(int x, int y, const std::wstring &s, WORD attr) {
        for (wchar_t ch : s) { Put(x, y, ch, attr); ++x; }
    }

    void Fill(int x, int y, int w, int h, wchar_t ch, WORD attr) {
        for (int yy = y; yy < y + h; ++yy)
            for (int xx = x; xx < x + w; ++xx)
                Put(xx, yy, ch, attr);
    }

    void HLine(int x, int y, int w, wchar_t ch, WORD attr) {
        for (int i = 0; i < w; ++i) Put(x + i, y, ch, attr);
    }

    void Flush() {
        if (!out_ || w_ <= 0 || h_ <= 0 || cells_.empty()) return;
        CONSOLE_SCREEN_BUFFER_INFO csbi{};
        if (!GetConsoleScreenBufferInfo(out_, &csbi)) return;
        SMALL_RECT region{};
        region.Left   = csbi.srWindow.Left;
        region.Top    = csbi.srWindow.Top;
        region.Right  = csbi.srWindow.Right;
        region.Bottom = csbi.srWindow.Bottom;
        COORD size{ (SHORT)w_, (SHORT)h_ };
        COORD origin{ 0, 0 };
        WriteConsoleOutputW(out_, cells_.data(), size, origin, &region);
    }

private:
    HANDLE out_ = nullptr;
    int w_ = 0, h_ = 0;
    std::vector<CHAR_INFO> cells_;
};

// -- The merged package list -------------------------------------------------

enum class Status { NotInstalled, Installed, Upgrade, LocalOnly, Unknown };
enum class View   { All, Installed, Upgrades };
enum class Mode   { Browse, Filter, Confirm };
enum class Pending { None, Remove, Purge, UpgradeAll };

struct Row {
    std::string name;
    std::string file;
    std::string desc;
    std::string author;
    std::string type;        // plugin / file / archive / program
    std::string local_ver;
    std::string repo_ver;
    IndexEntry  e;           // the repo's entry (what InstallPkg() needs)
    bool installed = false;
    bool in_repo   = false;
    bool folder    = false;
    Status status  = Status::NotInstalled;
};

const wchar_t *StatusText(Status s) {
    switch (s) {
        case Status::Installed: return L"installed";
        case Status::Upgrade:   return L"upgrade available";
        case Status::LocalOnly: return L"local only (not in the repo)";
        case Status::Unknown:   return L"installed, version unknown";
        default:                return L"not installed";
    }
}

WORD StatusAttr(Status s) {
    switch (s) {
        case Status::Installed: return kAttrGreen;
        case Status::Upgrade:   return kAttrYellow;
        case Status::LocalOnly: return kAttrCyan;
        case Status::Unknown:   return kAttrDim;
        default:                return kAttrText;
    }
}

wchar_t Marker(Status s) {
    switch (s) {
        case Status::Installed: return L'*';
        case Status::Upgrade:   return L'^';
        case Status::LocalOnly: return L'~';
        case Status::Unknown:   return L'?';
        default:                return L'+';
    }
}

class App {
public:
    int Run() {
        if (!IsConsole()) {
            std::fprintf(stderr, "hom: tui needs an interactive console "
                                 "(stdin/stdout are not a terminal).\n");
            return 1;
        }
        if (!InitConsole()) {
            std::fprintf(stderr, "hom: couldn't set up the console for the TUI.\n");
            return 1;
        }

        RebuildRows();
        SetStatus("Loading package index...", kAttrDim);
        Draw();
        SyncIndex(true);

        while (!quit_) {
            Draw();
            INPUT_RECORD rec{};
            DWORD n = 0;
            if (!ReadConsoleInputW(hin_, &rec, 1, &n)) break;
            if (rec.EventType == KEY_EVENT) HandleKey(rec.Event.KeyEvent);
        }

        RestoreConsole();
        return 0;
    }

private:
    // -- console setup -------------------------------------------------------
    bool InitConsole() {
        hout_orig_ = GetStdHandle(STD_OUTPUT_HANDLE);
        hin_       = GetStdHandle(STD_INPUT_HANDLE);
        if (!GetConsoleMode(hout_orig_, &old_out_mode_) || !GetConsoleMode(hin_, &old_in_mode_))
            return false;
        if (!GetConsoleCursorInfo(hout_orig_, &old_cursor_)) {
            old_cursor_.dwSize = 25;
            old_cursor_.bVisible = TRUE;
        }

        CONSOLE_SCREEN_BUFFER_INFO csbi{};
        GetConsoleScreenBufferInfo(hout_orig_, &csbi);

        hbuf_ = CreateConsoleScreenBuffer(GENERIC_READ | GENERIC_WRITE, 0, nullptr,
                                          CONSOLE_TEXTMODE_BUFFER, nullptr);
        if (hbuf_ == INVALID_HANDLE_VALUE) { hbuf_ = nullptr; return false; }
        SetConsoleScreenBufferSize(hbuf_, csbi.dwSize);
        if (!SetConsoleActiveScreenBuffer(hbuf_)) {
            CloseHandle(hbuf_);
            hbuf_ = nullptr;
            return false;
        }

        // No line input / echo / processed input: every key arrives as an
        // event (so Ctrl+C can be handled here instead of killing the app).
        SetConsoleMode(hin_, ENABLE_WINDOW_INPUT | ENABLE_EXTENDED_FLAGS);
        CONSOLE_CURSOR_INFO hide{ 1, FALSE };
        SetConsoleCursorInfo(hbuf_, &hide);
        scr_.Attach(hbuf_);
        return true;
    }

    void RestoreConsole() {
        if (hbuf_) {
            SetConsoleActiveScreenBuffer(hout_orig_);
            SetConsoleCursorInfo(hout_orig_, &old_cursor_);
            CloseHandle(hbuf_);
            hbuf_ = nullptr;
        }
        if (hin_) SetConsoleMode(hin_, old_in_mode_);
    }

    void HideCursor() {
        if (!hbuf_) return;
        CONSOLE_CURSOR_INFO hide{ 1, FALSE };
        SetConsoleCursorInfo(hbuf_, &hide);
    }

    // -- data ----------------------------------------------------------------
    void RebuildRows() {
        std::string keep = SelectedName();

        std::map<std::string, Row> merged;
        for (const auto &e : index_) {
            Row r;
            r.name     = e.name;
            r.file     = e.file.empty() ? (e.name + ".hrp") : e.file;
            r.type     = PkgType(e);
            r.e        = e;
            r.desc     = e.description;
            r.author   = e.author;
            r.repo_ver = e.version;
            r.in_repo  = true;
            merged[e.name] = std::move(r);
        }
        for (const auto &p : ListLocalPlugins()) {
            Row &r = merged[p.name];
            if (r.name.empty()) r.name = p.name;
            r.installed = true;
            r.folder    = (p.kind == "folder");
            if (r.type.empty()) r.type = (p.kind == "file" || p.kind == "archive" || p.kind == "program") ? p.kind : "plugin";
            r.local_ver = p.version;
            if (r.local_ver.empty()) { // installed this session: trust what we downloaded
                auto it = assumed_versions_.find(p.name);
                if (it != assumed_versions_.end()) r.local_ver = it->second;
            }
        }

        rows_.clear();
        for (auto &kv : merged) {
            Row &r = kv.second;
            if (r.file.empty() && r.type == "plugin") r.file = r.name + ".hrp";
            if (r.desc.empty() && !r.in_repo) r.desc = "Local package (not in the repo index).";
            if (!r.in_repo) {
                r.status = Status::LocalOnly;
            } else if (!r.installed) {
                r.status = Status::NotInstalled;
            } else if (r.local_ver.empty() || r.repo_ver.empty()) {
                r.status = Status::Unknown;
            } else {
                r.status = VersionGt(r.repo_ver, r.local_ver) ? Status::Upgrade : Status::Installed;
            }
            rows_.push_back(std::move(r));
        }
        RebuildView(keep);
    }

    void RebuildView(const std::string &keep = "") {
        std::string q = LowerAscii(filter_);
        view_.clear();
        for (size_t i = 0; i < rows_.size(); ++i) {
            const Row &r = rows_[i];
            if (view_mode_ == View::Installed && !r.installed) continue;
            if (view_mode_ == View::Upgrades && r.status != Status::Upgrade) continue;
            if (!q.empty() &&
                LowerAscii(r.name).find(q) == std::string::npos &&
                LowerAscii(r.desc).find(q) == std::string::npos) continue;
            view_.push_back(i);
        }
        cursor_ = 0;
        if (!keep.empty()) {
            for (size_t i = 0; i < view_.size(); ++i) {
                if (rows_[view_[i]].name == keep) { cursor_ = i; break; }
            }
        }
        if (cursor_ >= view_.size()) cursor_ = view_.empty() ? 0 : view_.size() - 1;
        if (top_ >= view_.size()) top_ = 0;
    }

    const Row *Selected() const {
        if (view_.empty() || cursor_ >= view_.size()) return nullptr;
        return &rows_[view_[cursor_]];
    }

    std::string SelectedName() const {
        const Row *r = Selected();
        return r ? r->name : std::string();
    }

    const Row *FindRow(const std::string &name) const {
        for (const auto &r : rows_) if (r.name == name) return &r;
        return nullptr;
    }

    void SetStatus(const std::string &s, WORD attr = kAttrText) {
        status_ = s;
        status_attr_ = attr;
    }

    // -- index ---------------------------------------------------------------
    void SyncIndex(bool initial) {
        SetStatus(initial ? "Loading package index..." : "Refreshing package index...", kAttrDim);
        Draw();

        bool ok = false;
        std::vector<IndexEntry> idx = FetchIndex(&ok);
        if (ok) {
            index_ = std::move(idx);
            RebuildRows();
            SetStatus("Index: " + std::to_string(index_.size()) + " package(s) from repo '" +
                      g_repo_name + "'.", kAttrGreen);
        } else {
            RebuildRows();
            SetStatus("Could not reach repo '" + g_repo_name +
                      "'; showing locally installed plugins only.", kAttrRed);
        }
    }

    // -- actions -------------------------------------------------------------
    bool InstallOne(const Row &row, std::string *msg) {
        if (row.folder) {
            *msg = "'" + row.name + "' is installed as a folder; remove it first "
                   "to switch to the repo package.";
            return false;
        }
        SetStatus("Downloading " + row.name + "...", kAttrYellow);
        Draw();

        // No questions here: a 'program' is only downloaded (and checked), never run from the TUI.
        InstallOpts o;
        o.yes = true;
        std::string err;
        InstallInfo r = InstallPkg(row.e, o, nullptr, &err);
        if (!r.ok) { *msg = err; return false; }
        if (!row.repo_ver.empty()) assumed_versions_[row.name] = row.repo_ver;

        std::string verb = !r.existed ? "Installed"
                          : (row.status == Status::Upgrade ? "Upgraded" : "Reinstalled");
        if (r.type == "program") verb = "Downloaded";
        *msg = verb + " " + row.name;
        if (!row.repo_ver.empty()) *msg += " " + row.repo_ver;
        if (r.type == "program") *msg += " - " + r.note;
        return true;
    }

    void InstallSelected() {
        const Row *sel = Selected();
        if (!sel) return;
        Row row = *sel; // actions rebuild rows_, so keep a copy

        if (!row.in_repo) {
            SetStatus("'" + row.name + "' is not in the repo index.", kAttrYellow);
            return;
        }
        if (row.folder) {
            SetStatus("'" + row.name + "' is installed as a folder; remove it first (r) "
                      "to switch to the repo package.", kAttrYellow);
            return;
        }

        std::string msg;
        if (InstallOne(row, &msg)) {
            RebuildRows();
            SetStatus(row.type == "plugin" ? msg + " - restart HomRec to load it." : msg, kAttrGreen);
        } else {
            SetStatus(msg, kAttrRed);
        }
    }

    void RequestRemove(bool purge) {
        const Row *r = Selected();
        if (!r || !r->installed) {
            SetStatus("Select an installed plugin first.", kAttrYellow);
            return;
        }
        pending_ = purge ? Pending::Purge : Pending::Remove;
        pending_name_ = r->name;
        mode_ = Mode::Confirm;
    }

    void DoRemove(bool purge) {
        std::string name = pending_name_;
        {
            std::string m; int rc = 0;
            if (RemoveDbPkg(name, &m, &rc)) {
                assumed_versions_.erase(name);
                RebuildRows();
                SetStatus(m, rc ? kAttrRed : kAttrGreen);
                return;
            }
        }
        FoundPluginPaths p = LocatePluginPaths(name);
        if (!p.has_hrp && !p.has_installed && !p.has_plain) {
            SetStatus("No plugin named '" + name + "' is installed here.", kAttrYellow);
            return;
        }

        if (p.has_hrp && !DeleteFileA(p.hrp_path.c_str())) {
            SetStatus("Couldn't delete '" + p.hrp_path + "' (error " +
                      std::to_string(GetLastError()) + ").", kAttrRed);
            return;
        }
        for (const std::string &dir : { p.installed_dir, p.plain_dir }) {
            if (!DirExistsA(dir)) continue;
            bool ok = purge ? DeleteDirRecursive(dir) : DeleteDirContentsExcept(dir, ".store");
            if (!ok) {
                SetStatus("Couldn't fully clean up '" + dir + "'.", kAttrRed);
                return;
            }
        }

        DbDel(name);
        assumed_versions_.erase(name);
        RebuildRows();
        SetStatus((purge ? "Purged " : "Removed ") + name +
                  (purge ? "." : " (saved settings kept; use p to purge)."), kAttrGreen);
    }

    void RequestUpgradeAll() {
        int n = 0;
        for (const auto &r : rows_) if (r.status == Status::Upgrade && !r.folder) ++n;
        if (!n) {
            SetStatus("Everything is up to date.", kAttrGreen);
            return;
        }
        pending_ = Pending::UpgradeAll;
        mode_ = Mode::Confirm;
    }

    void DoUpgradeAll() {
        std::vector<std::string> names;
        for (const auto &r : rows_) if (r.status == Status::Upgrade && !r.folder) names.push_back(r.name);

        int ok = 0, failed = 0;
        for (const auto &name : names) {
            const Row *r = FindRow(name);
            if (!r) continue;
            std::string msg;
            if (InstallOne(*r, &msg)) ++ok; else ++failed;
        }
        RebuildRows();
        SetStatus("Upgrade finished: " + std::to_string(ok) + " done, " +
                  std::to_string(failed) + " failed.", failed ? kAttrRed : kAttrGreen);
    }

    void StartFilter() {
        filter_before_ = filter_;
        mode_ = Mode::Filter;
        SetStatus("Type to filter; Enter applies, Esc cancels.", kAttrDim);
    }

    void MoveCursor(int delta) {
        if (view_.empty()) return;
        int c = (int)cursor_ + delta;
        if (c < 0) c = 0;
        if (c >= (int)view_.size()) c = (int)view_.size() - 1;
        cursor_ = (size_t)c;
    }

    int PageSize() const {
        int h = scr_.height() - 5;
        return h > 1 ? h : 1;
    }

    // -- input ---------------------------------------------------------------
    void HandleKey(const KEY_EVENT_RECORD &k) {
        if (!k.bKeyDown) return;

        if (mode_ == Mode::Confirm) {
            wchar_t c = (wchar_t)std::tolower((unsigned char)k.uChar.UnicodeChar);
            if (c == L'y') {
                Pending p = pending_;
                pending_ = Pending::None;
                mode_ = Mode::Browse;
                if (p == Pending::Remove) DoRemove(false);
                else if (p == Pending::Purge) DoRemove(true);
                else if (p == Pending::UpgradeAll) DoUpgradeAll();
            } else if (c == L'n' || k.wVirtualKeyCode == VK_ESCAPE ||
                       k.wVirtualKeyCode == VK_RETURN) {
                pending_ = Pending::None;
                mode_ = Mode::Browse;
                SetStatus("Cancelled.", kAttrDim);
            }
            return;
        }

        if (mode_ == Mode::Filter) {
            if (k.wVirtualKeyCode == VK_ESCAPE) {
                filter_ = filter_before_;
                mode_ = Mode::Browse;
                RebuildView();
                SetStatus("Filter cancelled.", kAttrDim);
            } else if (k.wVirtualKeyCode == VK_RETURN) {
                mode_ = Mode::Browse;
                SetStatus(filter_.empty() ? "Filter cleared." : "Filter: " + filter_, kAttrDim);
            } else if (k.wVirtualKeyCode == VK_BACK) {
                if (!filter_.empty()) {
                    filter_.pop_back();
                    RebuildView();
                }
            } else if (k.uChar.UnicodeChar >= 32 && k.uChar.UnicodeChar < 127) {
                filter_ += (char)k.uChar.UnicodeChar;
                RebuildView();
            }
            return;
        }

        switch (k.wVirtualKeyCode) {
            case VK_UP:    MoveCursor(-1); return;
            case VK_DOWN:  MoveCursor(1);  return;
            case VK_PRIOR: MoveCursor(-PageSize()); return;
            case VK_NEXT:  MoveCursor(PageSize());  return;
            case VK_HOME:  cursor_ = 0; return;
            case VK_END:   cursor_ = view_.empty() ? 0 : view_.size() - 1; return;
            case VK_TAB: {
                std::string keep = SelectedName();
                view_mode_ = view_mode_ == View::All ? View::Installed
                           : view_mode_ == View::Installed ? View::Upgrades : View::All;
                RebuildView(keep);
                SetStatus(ViewName() + ".", kAttrDim);
                return;
            }
            case VK_RETURN: InstallSelected(); return;
            case VK_ESCAPE:
                if (!filter_.empty()) {
                    filter_.clear();
                    RebuildView();
                    SetStatus("Filter cleared.", kAttrDim);
                }
                return;
            case VK_F5: SyncIndex(false); return;
            default: break;
        }

        wchar_t c = (wchar_t)std::tolower((unsigned char)k.uChar.UnicodeChar);
        if ((k.dwControlKeyState & (LEFT_CTRL_PRESSED | RIGHT_CTRL_PRESSED)) &&
            (c == L'c' || k.uChar.UnicodeChar == 3)) {
            quit_ = true;
            return;
        }
        switch (c) {
            case L'q': quit_ = true; break;
            case L'i': InstallSelected(); break;
            case L'r': RequestRemove(false); break;
            case L'p': RequestRemove(true); break;
            case L'u': RequestUpgradeAll(); break;
            case L'/': StartFilter(); break;
            case L's': SyncIndex(false); break;
            default: break;
        }
    }

    std::string ViewName() const {
        switch (view_mode_) {
            case View::Installed: return "View: installed";
            case View::Upgrades:  return "View: upgradable";
            default:              return "View: all packages";
        }
    }

    // -- drawing -------------------------------------------------------------
    void Draw() {
        scr_.Resize();
        const int W = scr_.width();
        const int H = scr_.height();
        scr_.Clear(kAttrText);

        if (W < 76 || H < 14) {
            std::wstring msg = L"Terminal too small: need at least 76x14, now " +
                               std::to_wstring(W) + L"x" + std::to_wstring(H) + L".";
            scr_.Text(1, 0, Fit(msg, W - 2), kAttrRed);
            scr_.Text(1, 2, L"Resize the window, or press q to quit.", kAttrDim);
            HideCursor();
            scr_.Flush();
            return;
        }

        int installed = 0, upgradable = 0;
        for (const auto &r : rows_) {
            if (r.installed) ++installed;
            if (r.status == Status::Upgrade) ++upgradable;
        }

        // Header
        scr_.Fill(0, 0, W, 1, L' ', kAttrHeader);
        scr_.Text(1, 0, std::wstring(L"hom ") + Widen(k_hom_version), kAttrHeader);
        std::wstring right = L"repo: " + Widen(g_repo_name) + L"   " +
                             std::to_wstring(rows_.size()) + L" packages   " +
                             std::to_wstring(installed) + L" installed   " +
                             std::to_wstring(upgradable) + L" upgradable";
        right = Fit(right, W - 14);
        scr_.Text(W - (int)right.size() - 1, 0, right, kAttrHeader);

        int split = W * 58 / 100;
        if (split > W - 32) split = W - 32;
        if (split < 42) split = 42;

        int ver_w = 20;
        if (split < 50) ver_w = 14;
        int ver_x = split - 1 - ver_w;
        int name_w = ver_x - 4;
        if (name_w < 8) name_w = 8;

        // Column headers
        scr_.Text(3, 1, L"NAME", kAttrDim);
        scr_.Text(ver_x, 1, PadLeft(L"VERSION", ver_w), kAttrDim);
        scr_.Text(split + 2, 1, L"DETAILS", kAttrDim);

        // Vertical separator
        for (int y = 1; y <= H - 3; ++y) scr_.Put(split, y, L'|', kAttrBox);

        // List
        int y0 = 2;
        int list_h = H - 4; // rows 2 .. H-3
        if (list_h < 1) list_h = 1;
        if (cursor_ < top_) top_ = cursor_;
        if ((int)cursor_ >= (int)top_ + list_h) top_ = cursor_ - (size_t)list_h + 1;
        if (top_ > 0 && view_.size() > (size_t)list_h) {
            scr_.Put(split - 2, 1, L'^', kAttrDim);
        }
        if (top_ + (size_t)list_h < view_.size()) {
            scr_.Put(split - 2, H - 3, L'v', kAttrDim);
        }

        for (size_t i = top_; i < view_.size() && (int)i < (int)top_ + list_h; ++i) {
            int y = y0 + (int)(i - top_);
            const Row &r = rows_[view_[i]];
            bool sel = (i == cursor_);
            WORD attr = sel ? kAttrSel : kAttrText;
            if (sel) scr_.Fill(0, y, split, 1, L' ', kAttrSel);
            scr_.Put(1, y, Marker(r.status), sel ? kAttrSel : StatusAttr(r.status));
            scr_.Text(3, y, Fit(Widen(r.name), name_w), attr);
            scr_.Text(ver_x, y, PadLeft(VersionCell(r), ver_w), attr);
        }
        if (view_.empty()) {
            scr_.Text(3, y0, filter_.empty() ? L"Nothing to show." : L"No packages match the filter.",
                      kAttrDim);
        }

        DrawDetails(split + 2, W - split - 3, H);

        // Status + help
        scr_.Fill(0, H - 2, W, 1, L' ', kAttrText);
        scr_.Text(1, H - 2, Fit(Widen(status_), W - 2), status_attr_);

        scr_.Fill(0, H - 1, W, 1, L' ', kAttrDim);
        if (mode_ == Mode::Filter) {
            scr_.Text(1, H - 1, L"Filter: ", kAttrBold);
            scr_.Text(9, H - 1, Fit(Widen(filter_), W - 30), kAttrBold);
            scr_.Text(10 + (int)filter_.size(), H - 1, L"  Enter apply, Esc cancel", kAttrDim);
        } else {
            const wchar_t *keys = W >= 100
                ? L"Up/Down move  i install  r remove  p purge  u upgrade all  / filter  Tab view  s sync  q quit"
                : L"Up/Dn move  i install  r remove  p purge  u upgrade  / filter  Tab  s sync  q quit";
            scr_.Text(1, H - 1, Fit(keys, W - 2), kAttrDim);
        }

        if (mode_ == Mode::Confirm) DrawConfirm(W, H);

        scr_.Flush();

        if (mode_ == Mode::Filter) {
            CONSOLE_SCREEN_BUFFER_INFO csbi{};
            if (GetConsoleScreenBufferInfo(hbuf_, &csbi)) {
                COORD pos{};
                pos.X = (SHORT)(csbi.srWindow.Left + 9 + (SHORT)filter_.size());
                pos.Y = (SHORT)(csbi.srWindow.Top + H - 1);
                SetConsoleCursorPosition(hbuf_, pos);
            }
            CONSOLE_CURSOR_INFO show{ 25, TRUE };
            SetConsoleCursorInfo(hbuf_, &show);
        } else {
            HideCursor();
        }
    }

    std::wstring VersionCell(const Row &r) const {
        if (r.status == Status::Upgrade) {
            return Widen(r.local_ver) + L" -> " + Widen(r.repo_ver);
        }
        if (r.installed) {
            return r.local_ver.empty() ? L"?" : Widen(r.local_ver);
        }
        return r.repo_ver.empty() ? L"-" : Widen(r.repo_ver);
    }

    void DrawDetails(int x, int w, int H) {
        if (w < 10) return;
        const Row *r = Selected();
        int y = 3; // row 1 is the pane header, row 2 is left blank
        if (!r) {
            scr_.Text(x, y + 1, L"No package selected.", kAttrDim);
            return;
        }

        auto field = [&](const wchar_t *label, const std::wstring &value, WORD attr) {
            if (y >= H - 3) return;
            scr_.Text(x, y, label, kAttrDim);
            scr_.Text(x + 12, y, Fit(value.empty() ? L"-" : value, w - 12), attr);
            ++y;
        };

        field(L"Name",     Widen(r->name), kAttrBold);
        field(L"Status",   StatusText(r->status), StatusAttr(r->status));
        field(L"Installed", r->local_ver.empty() ? (r->installed ? L"unknown" : L"not installed")
                                                 : Widen(r->local_ver),
              r->installed ? kAttrText : kAttrDim);
        field(L"Available", r->repo_ver.empty() ? L"-" : Widen(r->repo_ver), kAttrText);
        field(L"Author",    Widen(r->author), kAttrText);
        field(L"Package",   Widen(r->file), kAttrText);
        field(L"Kind",      r->folder ? L"plugin folder" : Widen(r->type.empty() ? "plugin" : r->type), kAttrText);

        y++;
        if (y < H - 3) {
            scr_.Text(x, y++, L"DESCRIPTION", kAttrDim);
            for (const auto &line : Wrap(Widen(r->desc), w)) {
                if (y >= H - 3) break;
                scr_.Text(x, y++, line, kAttrText);
            }
        }

        y++;
        const wchar_t *hint = nullptr;
        if (!r->in_repo)              hint = L"Local plugin: no repo package to install.";
        else if (r->folder)           hint = L"Folder install: remove it first to use the repo package.";
        else if (r->status == Status::Upgrade) hint = L"Press i to upgrade to the repo version.";
        else if (!r->installed)       hint = L"Press i to install.";
        else                          hint = L"Press i to reinstall, r to remove, p to purge.";
        if (hint && y < H - 3) scr_.Text(x, y, hint, kAttrDim);
    }

    void DrawConfirm(int W, int H) {
        std::wstring line1, line2, line3;
        if (pending_ == Pending::Remove) {
            line1 = L"Remove plugin '" + Widen(pending_name_) + L"'?";
            line2 = L"Saved settings (.store) are kept.";
            line3 = L"Use purge (p) instead to delete them too.";
        } else if (pending_ == Pending::Purge) {
            line1 = L"Purge plugin '" + Widen(pending_name_) + L"'?";
            line2 = L"The package, extracted files and saved settings";
            line3 = L"will all be deleted.";
        } else {
            int n = 0;
            for (const auto &r : rows_) if (r.status == Status::Upgrade && !r.folder) ++n;
            line1 = L"Upgrade " + std::to_wstring(n) + L" plugin(s)?";
            line2 = L"Each package is downloaded from repo '" + Widen(g_repo_name) + L"'.";
            line3 = L"HomRec needs a restart (or plugin reload) afterwards.";
        }

        int bw = (int)std::max({ line1.size(), line2.size(), line3.size() }) + 6;
        if (bw < 42) bw = 42;
        if (bw > W - 4) bw = W - 4;
        int bh = 7;
        int bx = (W - bw) / 2;
        int by = (H - bh) / 2;

        scr_.Fill(bx, by, bw, bh, L' ', kAttrDialog);
        scr_.HLine(bx, by, bw, L'-', kAttrDialog);
        scr_.HLine(bx, by + bh - 1, bw, L'-', kAttrDialog);
        for (int yy = by + 1; yy < by + bh - 1; ++yy) {
            scr_.Put(bx, yy, L'|', kAttrDialog);
            scr_.Put(bx + bw - 1, yy, L'|', kAttrDialog);
        }
        scr_.Put(bx, by, L'+', kAttrDialog);
        scr_.Put(bx + bw - 1, by, L'+', kAttrDialog);
        scr_.Put(bx, by + bh - 1, L'+', kAttrDialog);
        scr_.Put(bx + bw - 1, by + bh - 1, L'+', kAttrDialog);

        scr_.Text(bx + 2, by + 1, Fit(line1, bw - 4), kAttrDialog);
        scr_.Text(bx + 2, by + 2, Fit(line2, bw - 4), kAttrDialog);
        scr_.Text(bx + 2, by + 3, Fit(line3, bw - 4), kAttrDialog);
        scr_.Text(bx + 2, by + 5, L"[y] yes      [n] no", kAttrDialog);
    }

    // -- state ---------------------------------------------------------------
    HANDLE hout_orig_ = nullptr;
    HANDLE hin_ = nullptr;
    HANDLE hbuf_ = nullptr;
    DWORD  old_out_mode_ = 0;
    DWORD  old_in_mode_ = 0;
    CONSOLE_CURSOR_INFO old_cursor_{ 25, TRUE };
    Screen scr_;

    std::vector<IndexEntry> index_;
    std::vector<Row> rows_;
    std::vector<size_t> view_;
    std::map<std::string, std::string> assumed_versions_;

    size_t cursor_ = 0;
    size_t top_ = 0;
    View view_mode_ = View::All;
    Mode mode_ = Mode::Browse;
    Pending pending_ = Pending::None;
    std::string pending_name_;
    std::string filter_;
    std::string filter_before_;
    std::string status_;
    WORD status_attr_ = kAttrText;
    bool quit_ = false;
};

int RunTui() {
    App app;
    return app.Run();
}

} // namespace tui
#endif // _WIN32

// -----------------------------------------------------------------------
// Crash handler
// -----------------------------------------------------------------------

#ifdef _WIN32
namespace {

typedef BOOL(WINAPI *MiniDumpWriteDump_t)(
    HANDLE hProcess, DWORD ProcessId, HANDLE hFile, MINIDUMP_TYPE DumpType,
    PMINIDUMP_EXCEPTION_INFORMATION ExceptionParam,
    PMINIDUMP_USER_STREAM_INFORMATION UserStreamParam,
    PMINIDUMP_CALLBACK_INFORMATION CallbackParam);

LONG WINAPI HomSehFilter(EXCEPTION_POINTERS *info) {
    DWORD code = info && info->ExceptionRecord ? info->ExceptionRecord->ExceptionCode : 0;
    void *addr = info && info->ExceptionRecord ? info->ExceptionRecord->ExceptionAddress : nullptr;

    std::string crash_dir = ExeDir() + "\\crashes";
    CreateDirectoryA(crash_dir.c_str(), nullptr); // harmless no-op if it already exists

    time_t t = time(nullptr);
    tm lt{};
    localtime_s(&lt, &t);
    char stamp[32];
    strftime(stamp, sizeof(stamp), "%Y%m%d_%H%M%S", &lt);
    char dump_path[MAX_PATH];
    _snprintf_s(dump_path, _TRUNCATE, "%s\\hom_crash_%s.dmp", crash_dir.c_str(), stamp);

    bool dump_written = false;
    HMODULE dbghelp = LoadLibraryW(L"dbghelp.dll");
    if (dbghelp) {
        auto write_dump = (MiniDumpWriteDump_t)GetProcAddress(dbghelp, "MiniDumpWriteDump");
        if (write_dump) {
            HANDLE f = CreateFileA(dump_path, GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS,
                                    FILE_ATTRIBUTE_NORMAL, nullptr);
            if (f != INVALID_HANDLE_VALUE) {
                MINIDUMP_EXCEPTION_INFORMATION mei{};
                mei.ThreadId = GetCurrentThreadId();
                mei.ExceptionPointers = info;
                mei.ClientPointers = FALSE;
                dump_written = write_dump(GetCurrentProcess(), GetCurrentProcessId(), f,
                                           (MINIDUMP_TYPE)(MiniDumpNormal | MiniDumpWithDataSegs),
                                           &mei, nullptr, nullptr) != FALSE;
                CloseHandle(f);
            }
        }
        FreeLibrary(dbghelp);
    }

    std::fprintf(stderr, "hom: crashed -- unhandled exception 0x%08lX at %p%s%s\n",
                 code, addr,
                 dump_written ? " -- dump written: " : " -- (no dump written)",
                 dump_written ? dump_path : "");
    return EXCEPTION_EXECUTE_HANDLER; // let the process die cleanly, don't re-fault into WER
}

} // namespace
#endif // _WIN32

int main(int argc_in, char **argv_in) {
#ifdef _WIN32
    SetUnhandledExceptionFilter(HomSehFilter);
    // Unbuffered stdout: when HomRec's console runs us through a pipe, the C
    // runtime would otherwise hold everything back until exit - and a progress
    // bar that only appears when the download is over is not a progress bar.
    setvbuf(stdout, nullptr, _IONBF, 0);
#endif

    // Global options, accepted anywhere on the line and removed before the
    // command parsing below sees them: --progress=bar|lines|none
    std::vector<char *> kept;
    kept.push_back(argv_in[0]);
    bool progress_given = false;
    for (int i = 1; i < argc_in; ++i) {
        std::string a = argv_in[i];
        if (a.rfind("--progress=", 0) == 0) {
            std::string v = a.substr(11);
            if (v == "bar")        g_progress_mode = ProgressMode::Bar;
            else if (v == "lines") g_progress_mode = ProgressMode::Lines;
            else if (v == "none")  g_progress_mode = ProgressMode::None;
            else { std::fprintf(stderr, "hom: --progress must be bar, lines or none.\n"); return 1; }
            progress_given = true;
            continue;
        }
        kept.push_back(argv_in[i]);
    }
    int argc = (int)kept.size();
    char **argv = kept.data();
    if (!progress_given) {
        DWORD mode = 0;
        g_progress_mode = GetConsoleMode(GetStdHandle(STD_OUTPUT_HANDLE), &mode) ? ProgressMode::Bar : ProgressMode::None;
    }

    InitPaths(); // hom's own folders + the HomRec folder, before anything reads or writes them

    if (argc < 2) {
        // In a real terminal, plain `hom` opens the package browser; when
        // stdout is a pipe (scripts, HomRec's in-app console) keep printing
        // usage instead of trying to take over a screen that isn't there.
        if (tui::IsConsole()) {
            SelectActiveRepoFromConfig();
            return tui::RunTui();
        }
        PrintUsage();
        return 1;
    }

    std::string cmd = argv[1];

    if (cmd == "tui" || cmd == "ui") {
        SelectActiveRepoFromConfig();
        return tui::RunTui();
    }

    if (cmd == "--version" || cmd == "-v" || cmd == "-V") return CmdVersion();

    if (cmd == "where") { SelectActiveRepoFromConfig(); return CmdWhere(); }
    if (cmd == "clean") return CmdClean();
    if (cmd == "set") {
        if (argc < 3) { std::fprintf(stderr, "hom: usage: hom set <homrec|data> [folder|auto]\n"); return 1; }
        return CmdSet(argv[2], argc >= 4 ? argv[3] : "", argc >= 4);
    }

    // `hom repo ...` only reads/writes hom_repos.cfg - no network, so it's
    // handled before the active repo is even resolved.
    if (cmd == "repo") {
        std::string sub = argc >= 3 ? argv[2] : "list";
        if (sub == "list" || sub == "ls") return CmdRepoList();
        if (sub == "add")                 return CmdRepoAdd(argc >= 4 ? argv[3] : "", argc >= 5 ? argv[4] : "");
        if (sub == "set" || sub == "use") return CmdRepoSet(argc >= 4 ? argv[3] : "");
        if (sub == "remove" || sub == "rm" || sub == "delete") return CmdRepoRemove(argc >= 4 ? argv[3] : "");
        std::fprintf(stderr, "hom: unknown repo command '%s' (list, add, set, remove).\n\n", sub.c_str());
        PrintUsage();
        return 1;
    }

    // Everything below may touch the network: resolve the active repo once, and say so when it
    // isn't the official one (so nobody installs from a fork without noticing).
    if (cmd == "update" || cmd == "ping" || cmd == "upgrade" || cmd == "full-upgrade" ||
        cmd == "install" || cmd == "search" || cmd == "show" || cmd == "list") {
        SelectActiveRepoFromConfig();
        bool needs_repo = cmd != "list";
        for (int i = 2; i < argc; ++i) if (std::string(argv[i]) == "--upgradable") needs_repo = true;
        if (needs_repo && !homcommon::EqualsNoCase(g_repo_name, homcommon::kOfficialName))
            std::printf("hom: using repo '%s' (%s)\n", g_repo_name.c_str(), g_repo_url.c_str());
    }
    if (cmd == "update") {
        std::string positional; // update takes no positional arg, just flags
        Flags f = ParseHomFlags(argc, argv, 2, &positional);
        return CmdUpdate(f.force);
    }
    if (cmd == "ping")    return CmdPing();

    if (cmd == "upgrade") {
        std::string positional; // upgrade takes no positional arg, just flags
        Flags f = ParseHomFlags(argc, argv, 2, &positional);
        return CmdUpgrade(f.yes, f.force);
    }
    if (cmd == "full-upgrade") {
        std::string positional;
        Flags f = ParseHomFlags(argc, argv, 2, &positional);
        return CmdFullUpgrade(f.yes, f.force);
    }
    if (cmd == "autoremove") return CmdAutoremove();

    if (cmd == "install") {
        if (argc < 3) { std::fprintf(stderr, "hom: missing package name.\n\n"); PrintUsage(); return 1; }
        std::string name;
        Flags f = ParseHomFlags(argc, argv, 2, &name);
        if (name == "update-hrp") return CmdUpdateAllPlugins(); // old spelling of `hom upgrade`
        return CmdInstall(name, f);
    }
    if (cmd == "remove" || cmd == "uninstall") {
        if (argc < 3) { std::fprintf(stderr, "hom: missing package name.\n\n"); PrintUsage(); return 1; }
        std::string name;
        Flags f = ParseHomFlags(argc, argv, 2, &name);
        return CmdRemove(name, f.rflag);
    }
    if (cmd == "purge") {
        if (argc < 3) { std::fprintf(stderr, "hom: missing package name.\n\n"); PrintUsage(); return 1; }
        std::string name;
        Flags f = ParseHomFlags(argc, argv, 2, &name);
        return CmdPurge(name, f.rflag);
    }

    if (cmd == "search") {
        // Joined rather than a single ParseHomFlags positional, so an unquoted multi-word query
        // ("hom search input overlay") is treated as one query string instead of silently dropping
        // everything after the first word.
        std::string query;
        for (int i = 2; i < argc; ++i) { if (i > 2) query += ' '; query += argv[i]; }
        return CmdSearch(query);
    }
    if (cmd == "show") {
        if (argc < 3) { std::fprintf(stderr, "hom: missing package name.\n\n"); PrintUsage(); return 1; }
        return CmdShow(argv[2]);
    }
    if (cmd == "list") {
        bool installed = false, upgradable = false;
        for (int i = 2; i < argc; ++i) {
            std::string a = argv[i];
            if (a == "--installed") installed = true;
            else if (a == "--upgradable") upgradable = true;
        }
        if (!installed && !upgradable) {
            std::fprintf(stderr, "hom: list needs --installed or --upgradable.\n\n");
            PrintUsage();
            return 1;
        }
        return CmdList(upgradable);
    }

    if (cmd == "--help" || cmd == "-h" || cmd == "help") { PrintUsage(stdout); return 0; }

    std::fprintf(stderr, "hom: unknown command '%s'\n\n", cmd.c_str());
    PrintUsage();
    return 1;
}

#else // !_WIN32

int main() {
    std::fprintf(stderr, "hom: Windows-only for now (same as the rest of HomRec).\n");
    return 1;
}

#endif
