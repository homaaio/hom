-- now-playing/entry.lua
--
-- "nowplaying" console command that reads a "now playing" text file and
-- prints/toasts it. Adapted from the original "Spotify Web API" idea:
-- the Spotify Web API needs an "Authorization: Bearer <token>" header,
-- and homrec.http_get()/http_post() don't take custom headers (see
-- lua_api.cpp - http_post only lets you set Content-Type), so calling
-- Spotify's API directly isn't reachable from a plugin as-is. What IS
-- reachable: a lot of "now playing" tools (media-key loggers, some
-- Rainmeter/Spicetify setups, OBS-adjacent utilities) already write the
-- current track to a plain local .txt file - this plugin just reads
-- that, no auth needed. Point now_playing_file at yours.

local file_path = ""

homrec.register_setting(
    "now_playing_file",
    "Path to a local text file that always contains the current track (from Spotify/foobar2000/etc's own 'now playing' export feature)",
    function() return file_path end,
    function(raw) file_path = raw; homrec.store_set("file_path", raw) end
)

homrec.register_command("nowplaying", "Show the current track from now_playing_file", function(raw)
    if file_path == "" then
        homrec.print("now_playing_file is not set - point it at a text file your player keeps updated with the current track.")
        return
    end
    local f = io.open(file_path, "r")
    if not f then
        homrec.print("Couldn't open: " .. file_path)
        return
    end
    local line = f:read("*l") or ""
    f:close()
    if line == "" then
        homrec.print("(now_playing_file is empty)")
    else
        homrec.print("Now playing: " .. line)
        homrec.show_toast("Now playing: " .. line)
    end
end)

function on_load()
    file_path = homrec.store_get("file_path", "")
end
