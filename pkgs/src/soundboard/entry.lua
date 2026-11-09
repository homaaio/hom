-- soundboard/entry.lua
--
-- "play <name>" console command that plays a .wav from this plugin's
-- assets/ folder through your system's default output device. The Lua
-- API exposes no audio-mixing hook (no way to inject a sample straight
-- into HomRec's own mic/system audio mix from a plugin), so this shells
-- out to Windows' own SoundPlayer via PowerShell (io.popen has full
-- access - see bter's entry.lua for the same pattern with whoami/
-- ipconfig). That means it plays on real output hardware, not into the
-- recording pipeline directly - if you want it IN your recording, enable
-- "record system audio" in Settings > Audio so it gets picked up the
-- same way any other desktop sound would.
--
-- Add your own clips: drop foo.wav in assets/, then "play foo".

local function assets_dir()
    return "plugins\\soundboard-src\\assets\\"
end

homrec.register_command("play", "Play a .wav from this plugin's assets/ folder: play <name>", function(raw)
    local _, name = raw:match("^(%S*)%s*(.-)$")
    if name == "" then
        homrec.print("Usage: play <name>  (looks for assets\\<name>.wav)")
        return
    end
    local path = assets_dir() .. name .. ".wav"
    local f = io.open(path, "rb")
    if not f then
        homrec.print("Not found: " .. path)
        return
    end
    f:close()
    -- -WindowStyle Hidden keeps this from flashing a console window.
    local cmd = string.format(
        'powershell -NoProfile -WindowStyle Hidden -Command "(New-Object Media.SoundPlayer \'%s\').PlaySync()"',
        path)
    os.execute(cmd)
end)

homrec.register_command("soundlist", "List available soundboard clips", function(raw)
    local p = io.popen('dir /b "' .. assets_dir() .. '*.wav" 2> nul')
    if not p then homrec.print("(couldn't list assets folder)"); return end
    local out = p:read("*a")
    p:close()
    if out == "" or out == nil then
        homrec.print("No .wav files in assets\\ yet.")
    else
        homrec.print("Clips:")
        for line in out:gmatch("[^\r\n]+") do
            homrec.print("  " .. line:gsub("%.wav$", ""))
        end
    end
end)
