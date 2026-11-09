-- profiles/entry.lua
--
-- Named snapshots of the settings you tend to flip together - resolution,
-- fps, codec, quality, bitrate, mic - saved/restored with one command:
--   profile save streaming
--   profile load streaming
--   profile list
-- Pairs well with the built-in custom hotkeys (Settings > Hotkeys >
-- Custom): bind Ctrl+1 -> "profile load streaming", Ctrl+2 -> "profile
-- load archival", etc. This is the practical version of the original
-- "scene switcher" / "hotkey macro" ideas - a plugin can only drive
-- values through homrec.settings_get/settings_set (there's no
-- homrec.run_command() to chain arbitrary console commands or touch
-- overlay visibility from Lua), so a "profile" here is a settings
-- snapshot, not a full OBS-style scene with overlay layout.

local FIELDS = {
    "resolution_w", "resolution_h", "target_fps", "video_codec",
    "quality", "enc_crf", "enc_preset", "hw_accel",
    "audio_aac_bitrate", "mic_device_id",
}

local function store_key(name, field) return "profile_" .. name .. "_" .. field end

homrec.register_command("profile", "profile save|load|list [name]", function(raw)
    local _, rest = raw:match("^(%S*)%s*(.-)$")
    local sub, name = rest:match("^(%S*)%s*(.-)$")

    if sub == "save" then
        if name == "" then homrec.print("Usage: profile save <name>"); return end
        for _, field in ipairs(FIELDS) do
            local v = homrec.settings_get(field)
            homrec.store_set(store_key(name, field), tostring(v))
        end
        local names = homrec.store_get("profile_names", "")
        if not names:find("|" .. name .. "|", 1, true) then
            homrec.store_set("profile_names", names .. "|" .. name .. "|")
        end
        homrec.print("Saved profile: " .. name)

    elseif sub == "load" then
        if name == "" then homrec.print("Usage: profile load <name>"); return end
        local names = homrec.store_get("profile_names", "")
        if not names:find("|" .. name .. "|", 1, true) then
            homrec.print("No such profile: " .. name .. " (see: profile list)")
            return
        end
        for _, field in ipairs(FIELDS) do
            local v = homrec.store_get(store_key(name, field), nil)
            if v ~= nil then homrec.settings_set(field, v) end
        end
        homrec.print("Loaded profile: " .. name)
        homrec.show_toast("Profile loaded: " .. name)

    elseif sub == "list" then
        local names = homrec.store_get("profile_names", "")
        local any = false
        for n in names:gmatch("|([^|]+)|") do
            homrec.print("  " .. n)
            any = true
        end
        if not any then homrec.print("(no profiles saved yet - profile save <name>)") end

    else
        homrec.print("Usage: profile save|load|list [name]")
    end
end)
