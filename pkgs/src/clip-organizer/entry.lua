-- clip-organizer/entry.lua
--
-- Fires on the on_replay_saved hook (Instant Replay's Save Replay hotkey/
-- command) and copies the newest file in output_folder into a same-day
-- "Clips\YYYY-MM-DD\" subfolder, so replay saves don't pile up loose
-- next to your full recordings. Adapted from the original "auto-clip
-- cuts the last N seconds" idea: there's no Lua-exposed API to trigger a
-- buffer cut on demand (that's what the C++-side Instant Replay feature
-- already does via its own hotkey/command) or to know the exact filename
-- on_replay_saved just wrote (the hook takes no arguments) - so this
-- takes the pragmatic route of "find the most recently modified file in
-- the output folder right after the hook fires" instead.
--
-- Full io/os access (see bter's entry.lua) is what makes the file-move
-- possible without any dedicated homrec.* call for it.

local function newest_file(folder)
    local p = io.popen('powershell -NoProfile -Command "Get-ChildItem -Path \'' .. folder ..
        '\' -File | Sort-Object LastWriteTime -Descending | Select-Object -First 1 -ExpandProperty FullName"')
    if not p then return nil end
    local out = p:read("*a")
    p:close()
    out = out and out:gsub("[\r\n]+$", "") or ""
    if out == "" then return nil end
    return out
end

local function today_stamp()
    return os.date("%Y-%m-%d")
end

function on_replay_saved()
    local folder = homrec.settings_get("output_folder")
    if not folder or folder == "" or folder == false then
        homrec.log("clip-organizer: output_folder not set, skipping")
        return
    end
    local src = newest_file(folder)
    if not src then
        homrec.log("clip-organizer: couldn't find the just-saved file in " .. folder)
        return
    end
    local dest_dir = folder .. "\\Clips\\" .. today_stamp()
    os.execute('mkdir "' .. dest_dir .. '" 2> nul')
    local name = src:match("[^\\]+$") or src
    local dest = dest_dir .. "\\" .. name
    local ok, err = os.rename(src, dest)
    if ok then
        homrec.show_toast("Replay filed under Clips\\" .. today_stamp())
        homrec.log("clip-organizer: moved " .. src .. " -> " .. dest)
    else
        homrec.log("clip-organizer: move failed: " .. tostring(err))
    end
end
