-- watermark/entry.lua
--
-- One-click logo watermark. The Lua plugin API has no call to reach into
-- the CURRENT recording session's overlay list (homrec.* has no
-- add_overlay()/set_overlay()), and homrec.register_input_overlay() turns
-- out to be specific to the Input-Overlay feature (keyboard/mouse/gamepad
-- mapping JSON - see plugins/input_overlay_presets/entry.lua's asset
-- format), not a generic "add a static image overlay" registration - so
-- it's the wrong tool for a logo. What this plugin actually does instead:
--   1. registers watermark_path/watermark_opacity as real settings
--      (console, autoexec.cfg, or a custom hotkey - Settings > Hotkeys >
--      Custom - can all set them), so your logo path and preferred
--      opacity are remembered like any built-in .hrc field, and
--   2. a "watermark" console command that prints the resolved path so you
--      can paste it straight into Overlays > + > Image's file picker
--      (still a manual step - see above for why - but a one-liner
--      instead of hunting for the file each time).
--
-- Drop your logo anywhere and point watermark_path at it (or leave blank
-- and put it at assets/logo.png next to this file).

local settings = { path = "", opacity = "80" }

homrec.register_setting(
    "watermark_path",
    "Path to your logo PNG (blank = this plugin's own assets/logo.png)",
    function() return settings.path end,
    function(raw) settings.path = raw; homrec.store_set("path", raw) end
)

homrec.register_setting(
    "watermark_opacity",
    "Reminder value for the opacity you want on the Image overlay (0-100) - not applied automatically, see the \"watermark\" command",
    function() return settings.opacity end,
    function(raw) settings.opacity = raw; homrec.store_set("opacity", raw) end
)

homrec.register_command("watermark",
    "Print the resolved watermark logo path + reminder opacity, to paste into Overlays > + > Image",
    function(raw)
        local path = settings.path
        if path == "" then path = "plugins\\watermark-src\\assets\\logo.png" end
        homrec.print("Logo path: " .. path)
        homrec.print("Suggested opacity: " .. settings.opacity .. " (set it on the overlay's \"Edit Parameters...\")")
    end
)

function on_load()
    settings.path = homrec.store_get("path", "")
    settings.opacity = homrec.store_get("opacity", "80")
end
