// common.h - small helpers for hom.exe: repos, config files, download progress line.
#pragma once

#include <algorithm>
#include <cctype>
#include <cstdio>
#include <cstdlib>
#include <map>
#include <filesystem>
#include <fstream>
#include <string>
#include <vector>

namespace homcommon {

// ---------------------------------------------------------------------------
// Repositories
// ---------------------------------------------------------------------------

constexpr char kOfficialName[] = "official";
constexpr char kOfficialUrl[]  = "https://raw.githubusercontent.com/homaaio/hom/main/pkgs/";
constexpr char kCfgDirName[]   = "cfg";
constexpr char kRepoCfgName[]  = "hom_repos.cfg";
constexpr char kCfgName[]      = "hom.cfg";      // key/value settings: homrec, data
constexpr char kDbName[]       = "hom_db.cfg";   // what hom installed (non-plugin packages, plugin versions)

// hom updates itself only from here, never from the selected package repo.
constexpr char kSelfVerUrl[]   = "https://github.com/homaaio/hom/releases/latest/download/version.txt";
constexpr char kSelfExeUrl[]   = "https://github.com/homaaio/hom/releases/latest/download/hom.exe";
constexpr char kSelfShaUrl[]   = "https://github.com/homaaio/hom/releases/latest/download/hom.exe.sha256";

struct Repo {
    std::string name;
    std::string url; // always https, always ends with '/'
};

struct RepoConfig {
    std::string       active = kOfficialName;
    std::vector<Repo> repos; // user-defined only; "official" is implicit
};

inline std::string TrimStr(const std::string &s) {
    size_t a = s.find_first_not_of(" \t\r\n");
    if (a == std::string::npos) return "";
    size_t b = s.find_last_not_of(" \t\r\n");
    return s.substr(a, b - a + 1);
}

inline bool EqualsNoCase(const std::string &a, const std::string &b) {
    if (a.size() != b.size()) return false;
    for (size_t i = 0; i < a.size(); ++i)
        if (std::tolower((unsigned char)a[i]) != std::tolower((unsigned char)b[i])) return false;
    return true;
}

// Repo names are used on the command line and in hom_repos.cfg, so keep them
// boring: letters, digits, '.', '-', '_' (no spaces, no separators).
inline bool IsValidRepoName(const std::string &name, std::string *why) {
    if (name.empty())      { *why = "name is empty"; return false; }
    if (name.size() > 32)  { *why = "name is longer than 32 characters"; return false; }
    for (unsigned char c : name) {
        if (!(std::isalnum(c) || c == '.' || c == '-' || c == '_')) {
            *why = "only letters, digits, '.', '-' and '_' are allowed";
            return false;
        }
    }
    if (EqualsNoCase(name, kOfficialName)) { *why = "'official' is reserved"; return false; }
    return true;
}

// Turns what the user typed into the canonical base URL of a repo: the folder that holds
// index.json and the package files (pkgs/ in the hom repo).
// Accepts: a https:// URL, or GitHub shorthand owner/repo[@branch] (-> .../branch/pkgs/).
inline bool NormalizeRepoUrl(const std::string &input, std::string *out, std::string *why) {
    std::string s = TrimStr(input);
    if (s.empty()) { *why = "URL is empty"; return false; }
    for (unsigned char c : s) {
        if (c <= 0x20 || c == 0x7F || c == '"' || c == '\'' || c == '\\' || c == '<' || c == '>') {
            *why = "URL contains a space or an illegal character";
            return false;
        }
    }
    if (s.find("..") != std::string::npos) { *why = "URL contains '..'"; return false; }

    std::string lower = s;
    std::transform(lower.begin(), lower.end(), lower.begin(), [](unsigned char c) { return (char)std::tolower(c); });

    if (lower.compare(0, 7, "http://") == 0) {
        *why = "only https:// repositories are allowed (hom downloads executables from them)";
        return false;
    }
    if (lower.compare(0, 8, "https://") == 0) {
        size_t host_end = s.find('/', 8);
        std::string host = s.substr(8, host_end == std::string::npos ? std::string::npos : host_end - 8);
        if (host.empty() || host.find('@') != std::string::npos) {
            *why = "URL has no valid host";
            return false;
        }
        *out = s;
        if (out->back() != '/') out->push_back('/');
        return true;
    }
    if (s.find("://") != std::string::npos) {
        *why = "only https:// repositories are allowed";
        return false;
    }

    // owner/repo[@branch]
    std::string branch = "main";
    std::string repo_part = s;
    size_t at = s.find('@');
    if (at != std::string::npos) {
        repo_part = s.substr(0, at);
        branch = s.substr(at + 1);
        if (branch.empty()) { *why = "branch after '@' is empty"; return false; }
    }
    size_t slash = repo_part.find('/');
    if (slash == std::string::npos || slash == 0 || slash + 1 >= repo_part.size() ||
        repo_part.find('/', slash + 1) != std::string::npos) {
        *why = "expected a https:// URL or GitHub shorthand 'owner/repo' (or 'owner/repo@branch')";
        return false;
    }
    for (unsigned char c : repo_part) {
        if (!(std::isalnum(c) || c == '.' || c == '-' || c == '_' || c == '/')) {
            *why = "owner/repo contains an illegal character";
            return false;
        }
    }
    for (unsigned char c : branch) {
        if (!(std::isalnum(c) || c == '.' || c == '-' || c == '_' || c == '/')) {
            *why = "branch contains an illegal character";
            return false;
        }
    }
    *out = "https://raw.githubusercontent.com/" + repo_part + "/" + branch + "/pkgs/";
    return true;
}

// Where hom keeps its own files (repo list, hom.cfg, install database).
//  - next to HomRec (hr.exe / homrec.exe beside hom.exe), or when cfg\ or hom.portable exists
//    next to hom.exe: <exe dir>\cfg  (portable, the way it always was)
//  - otherwise (hom installed on its own): %LOCALAPPDATA%\hom
inline std::filesystem::path StateDir(const std::filesystem::path &exe_dir) {
    namespace fs = std::filesystem;
    std::error_code ec;
    fs::path portable = exe_dir / kCfgDirName;
    if (fs::exists(exe_dir / "hr.exe", ec) || fs::exists(exe_dir / "homrec.exe", ec) ||
        fs::exists(portable, ec) || fs::exists(exe_dir / "hom.portable", ec))
        return portable;
    const char *la = std::getenv("LOCALAPPDATA");
    if (la && *la) return fs::path(la) / "hom";
    return portable;
}

inline std::filesystem::path RepoCfgFile(const std::filesystem::path &state_dir) {
    namespace fs = std::filesystem;
    fs::path neu = state_dir / kRepoCfgName;
    fs::path old = state_dir.parent_path() / kRepoCfgName; // pre-cfg\ location
    std::error_code ec;
    if (!fs::exists(neu, ec) && fs::exists(old, ec)) {
        fs::create_directories(neu.parent_path(), ec);
        fs::rename(old, neu, ec);
        if (ec) { ec.clear(); fs::copy_file(old, neu, ec); if (!ec) fs::remove(old, ec); }
    }
    return neu;
}

inline RepoConfig LoadRepoConfig(const std::filesystem::path &path) {
    RepoConfig cfg;
    std::ifstream f(path);
    if (!f) return cfg;
    std::string line;
    while (std::getline(f, line)) {
        line = TrimStr(line);
        if (line.empty() || line[0] == '#') continue;
        size_t sp = line.find_first_of(" \t");
        std::string key = sp == std::string::npos ? line : line.substr(0, sp);
        std::string rest = sp == std::string::npos ? "" : TrimStr(line.substr(sp + 1));
        if (key == "active") {
            if (!rest.empty()) cfg.active = rest;
        } else if (key == "repo") {
            size_t sp2 = rest.find_first_of(" \t");
            if (sp2 == std::string::npos) continue;
            Repo r;
            r.name = rest.substr(0, sp2);
            std::string why, url;
            if (!IsValidRepoName(r.name, &why)) continue;
            if (!NormalizeRepoUrl(rest.substr(sp2 + 1), &url, &why)) continue; // hand-edited junk is skipped, not trusted
            r.url = url;
            bool dup = false;
            for (const auto &e : cfg.repos) if (EqualsNoCase(e.name, r.name)) dup = true;
            if (!dup) cfg.repos.push_back(std::move(r));
        }
    }
    return cfg;
}

inline bool SaveRepoConfig(const std::filesystem::path &path, const RepoConfig &cfg) {
    std::error_code ec;
    if (path.has_parent_path()) std::filesystem::create_directories(path.parent_path(), ec); // cfg/ may not exist yet
    std::ofstream f(path, std::ios::trunc);
    if (!f) return false;
    f << "# hom repositories - edit with `hom repo add/set/remove`, or by hand.\n";
    f << "# \"official\" is built in and is used when 'active' is missing or unknown.\n";
    f << "active " << cfg.active << "\n";
    for (const auto &r : cfg.repos) f << "repo " << r.name << " " << r.url << "\n";
    return f.good();
}

inline const Repo *FindRepo(const RepoConfig &cfg, const std::string &name) {
    for (const auto &r : cfg.repos) if (EqualsNoCase(r.name, name)) return &r;
    return nullptr;
}

// The repo that's actually in effect: the active one if it still exists,
// otherwise the official one.
inline Repo ActiveRepo(const RepoConfig &cfg) {
    if (!EqualsNoCase(cfg.active, kOfficialName)) {
        if (const Repo *r = FindRepo(cfg, cfg.active)) return *r;
    }
    return Repo{kOfficialName, kOfficialUrl};
}

// ---------------------------------------------------------------------------
// hom.cfg: "key value" per line (keys: homrec, data)
// ---------------------------------------------------------------------------

inline std::map<std::string, std::string> LoadKv(const std::filesystem::path &path) {
    std::map<std::string, std::string> kv;
    std::ifstream f(path);
    std::string line;
    while (f && std::getline(f, line)) {
        line = TrimStr(line);
        if (line.empty() || line[0] == '#') continue;
        size_t sp = line.find_first_of(" \t");
        if (sp == std::string::npos) continue;
        kv[line.substr(0, sp)] = TrimStr(line.substr(sp + 1));
    }
    return kv;
}

inline bool SaveKv(const std::filesystem::path &path, const std::map<std::string, std::string> &kv) {
    std::error_code ec;
    if (path.has_parent_path()) std::filesystem::create_directories(path.parent_path(), ec);
    std::ofstream f(path, std::ios::trunc);
    if (!f) return false;
    f << "# hom settings - edit with `hom set <key> <value>`, or by hand.\n";
    for (const auto &e : kv) f << e.first << " " << e.second << "\n";
    return f.good();
}

// ---------------------------------------------------------------------------
// hom_db.cfg: one line per installed package, fields separated by '|':
//   name|type|version|path        (path = file or folder hom put on disk)
// ---------------------------------------------------------------------------

struct DbRec {
    std::string name, type, version, path;
};

inline std::vector<DbRec> LoadDb(const std::filesystem::path &path) {
    std::vector<DbRec> out;
    std::ifstream f(path);
    std::string line;
    while (f && std::getline(f, line)) {
        line = TrimStr(line);
        if (line.empty() || line[0] == '#') continue;
        std::vector<std::string> p;
        size_t a = 0;
        for (;;) {
            size_t b = line.find('|', a);
            p.push_back(line.substr(a, b == std::string::npos ? std::string::npos : b - a));
            if (b == std::string::npos) break;
            a = b + 1;
        }
        if (p.size() < 4 || p[0].empty()) continue;
        out.push_back({p[0], p[1], p[2], p[3]});
    }
    return out;
}

inline bool SaveDb(const std::filesystem::path &path, const std::vector<DbRec> &recs) {
    std::error_code ec;
    if (path.has_parent_path()) std::filesystem::create_directories(path.parent_path(), ec);
    std::ofstream f(path, std::ios::trunc);
    if (!f) return false;
    f << "# hom install database - managed by hom, don't edit while hom runs.\n";
    for (const auto &r : recs) f << r.name << "|" << r.type << "|" << r.version << "|" << r.path << "\n";
    return f.good();
}

// ---------------------------------------------------------------------------
// Download progress line
// ---------------------------------------------------------------------------

inline std::string FormatBytes(long long b) {
    char buf[32];
    if (b < 0) b = 0;
    if (b < 1024)                    std::snprintf(buf, sizeof(buf), "%lld B", b);
    else if (b < 1024LL * 1024)      std::snprintf(buf, sizeof(buf), "%.1f KB", b / 1024.0);
    else                             std::snprintf(buf, sizeof(buf), "%.1f MB", b / (1024.0 * 1024.0));
    return buf;
}

// One line, ASCII only, no leading '\r' / trailing '\n':
inline std::string FormatProgress(int pct, long long received, long long total, int eta_sec) {
    std::string s;
    if (pct >= 0 && total > 0) {
        if (pct > 100) pct = 100;
        const int width = 20;
        int filled = pct * width / 100;
        s += "[";
        s.append((size_t)filled, '#');
        s.append((size_t)(width - filled), '-');
        char buf[32];
        std::snprintf(buf, sizeof(buf), "] %3d%%  ", pct);
        s += buf;
        s += FormatBytes(received) + " / " + FormatBytes(total);
    } else {
        s += "downloading...  " + FormatBytes(received);
    }
    if (eta_sec >= 0) {
        char buf[32];
        if (eta_sec >= 60) std::snprintf(buf, sizeof(buf), "  ~%dm %02ds", eta_sec / 60, eta_sec % 60);
        else               std::snprintf(buf, sizeof(buf), "  ~%ds", eta_sec);
        s += buf;
    }
    return s;
}

} // namespace homcommon
