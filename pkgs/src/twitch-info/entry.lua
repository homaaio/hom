-- twitch-info/entry.lua
--
-- Adapted from the original "Twitch chat overlay" idea. The real Twitch
-- Helix API needs an OAuth bearer token (a header homrec.http_get/post
-- can't set - see webhook-notify's and now-playing's header comments for
-- the same limitation), and Twitch IRC chat needs a persistent WebSocket
-- connection, which isn't something this plugin sandbox can hold open
-- (no timers/background sockets exposed to Lua - only one-shot
-- http_get/http_post calls made synchronously from a console command).
-- decapi.me is a small public bridge that answers plain, unauthenticated
-- GET requests for a handful of read-only Twitch facts, which fits what
-- IS available here. Not affiliated with Twitch or decapi.me - it's a
-- third-party convenience service, so treat it as "usually works," not
-- guaranteed uptime.

homrec.register_setting(
    "twitch_channel",
    "Twitch channel name to look up with the twitch* commands",
    function() return homrec.store_get("channel", "") end,
    function(raw) homrec.store_set("channel", raw) end
)

local function channel()
    local c = homrec.store_get("channel", "")
    return c
end

local function get_decapi(path)
    local c = channel()
    if c == "" then
        homrec.print("twitch_channel is not set - run: twitch_channel = <your channel name>")
        return nil
    end
    local resp, err = homrec.http_get("https://decapi.me/twitch/" .. path .. "/" .. c)
    if not resp then
        homrec.print("Request failed: " .. tostring(err))
        return nil
    end
    return resp:gsub("%s+$", "")
end

homrec.register_command("twitchtitle", "Show the configured channel's stream title", function(raw)
    local v = get_decapi("title")
    if v then homrec.print("Title: " .. v) end
end)

homrec.register_command("twitchuptime", "Show how long the configured channel has been live (or offline)", function(raw)
    local v = get_decapi("uptime")
    if v then homrec.print("Uptime: " .. v) end
end)

homrec.register_command("twitchfollowers", "Show the configured channel's follower count", function(raw)
    local v = get_decapi("followcount")
    if v then homrec.print("Followers: " .. v) end
end)
