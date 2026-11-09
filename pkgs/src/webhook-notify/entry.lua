-- webhook-notify/entry.lua
--
-- Posts a small JSON message to a Discord or Slack "Incoming Webhook" URL
-- when recording starts/stops or an Instant Replay is saved. Deliberately
-- NOT an actual video uploader (the original "auto-upload" idea): homrec.
-- http_post() takes the whole body as one in-memory Lua string with no
-- streaming, so posting a multi-GB recording through it would mean
-- io.open():read("*a")-ing the entire file into RAM first - fine for a
-- short clip, a bad idea for anything long. A webhook ping (with the
-- local file path included, so you can grab it yourself) is the
-- realistic version of "tell me/my team when a recording's ready".
--
-- Setup: set webhook_url to your Discord/Slack webhook, e.g.
--   webhook_url = https://discord.com/api/webhooks/XXXX/YYYY
-- Discord and Slack both accept {"content": "..."} as the JSON body, so
-- one code path covers either.

local webhook_url = ""

homrec.register_setting(
    "webhook_url",
    "Discord/Slack incoming-webhook URL to notify on record start/stop/replay-saved",
    function() return webhook_url end,
    function(raw) webhook_url = raw; homrec.store_set("webhook_url", raw) end
)

local function escape_json(s)
    return (s:gsub('[\\"\n\r\t]', {
        ['\\'] = '\\\\', ['"'] = '\\"', ['\n'] = '\\n', ['\r'] = '', ['\t'] = '\\t',
    }))
end

local function notify(text)
    if webhook_url == "" then return end
    local body = '{"content":"' .. escape_json(text) .. '"}'
    local resp, err = homrec.http_post(webhook_url, body, "application/json")
    if not resp then
        homrec.log("webhook-notify: post failed: " .. tostring(err))
    end
end

function on_load()
    webhook_url = homrec.store_get("webhook_url", "")
end

function on_recording_start()
    notify("HomRec: recording started (" .. tostring(homrec.settings_get("resolution_w")) .. "x" ..
           tostring(homrec.settings_get("resolution_h")) .. " @" .. tostring(homrec.settings_get("target_fps")) .. "fps)")
end

function on_recording_stop()
    notify("HomRec: recording stopped, saving to " .. tostring(homrec.settings_get("output_folder")))
end

function on_replay_saved()
    notify("HomRec: Instant Replay saved to " .. tostring(homrec.settings_get("output_folder")))
end

homrec.register_command("webhook", "Send a test notification to the configured webhook_url", function(raw)
    if webhook_url == "" then
        homrec.print("webhook_url is not set - run: webhook_url = <your Discord/Slack webhook URL>")
        return
    end
    notify("HomRec: test notification from the \"webhook\" console command.")
    homrec.print("Sent.")
end)
