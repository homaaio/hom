-- diag-tools/entry.lua
--
-- A handful of "what's going on with this machine" console commands,
-- for troubleshooting the kind of low-end-hardware/driver issues that
-- come up around capture (CPU load, GPU/driver info, free disk space
-- where recordings are being written, which window has focus). All via
-- io.popen shelling out to stock Windows tools/PowerShell (see bter's
-- entry.lua for the same io/os-access pattern) - no dedicated homrec.*
-- API needed for any of this.

local function run(cmd)
    local p = io.popen(cmd .. " 2> nul")
    if not p then return "(failed to run)" end
    local out = p:read("*a") or ""
    p:close()
    return out
end

homrec.register_command("gpuinfo", "Show GPU name/driver version (wmic)", function(raw)
    local out = run('wmic path win32_VideoController get Name,DriverVersion /format:list')
    homrec.print(out ~= "" and out or "(no output)")
end)

homrec.register_command("cpuload", "Show current CPU load percentage", function(raw)
    local out = run('powershell -NoProfile -Command "Get-CimInstance Win32_Processor | Select-Object -ExpandProperty LoadPercentage"')
    homrec.print("CPU load: " .. (out:gsub("%s+$", "")) .. "%")
end)

homrec.register_command("diskfree", "Show free space on the output folder's drive", function(raw)
    local folder = homrec.settings_get("output_folder")
    if not folder or folder == false or folder == "" then
        homrec.print("output_folder is not set")
        return
    end
    local drive = folder:match("^(%a:)") or "C:"
    local out = run(string.format(
        'powershell -NoProfile -Command "Get-PSDrive %s | Select-Object -ExpandProperty Free"', drive:sub(1,1)))
    local bytes = tonumber((out:gsub("%s+", "")))
    if bytes then
        homrec.print(string.format("Free on %s: %.1f GB", drive, bytes / (1024*1024*1024)))
    else
        homrec.print("Couldn't read free space for " .. drive)
    end
end)

homrec.register_command("activewindow", "Show the title of the currently focused window", function(raw)
    local out = run('powershell -NoProfile -Command "Add-Type -AssemblyName Microsoft.VisualBasic; ' ..
        '$sig=\'[DllImport(\"user32.dll\")] public static extern IntPtr GetForegroundWindow(); ' ..
        '[DllImport(\"user32.dll\")] public static extern int GetWindowText(IntPtr h, System.Text.StringBuilder s, int n);\'; ' ..
        'Add-Type -MemberDefinition $sig -Name Win32 -Namespace Diag; ' ..
        '$h=[Diag.Win32]::GetForegroundWindow(); $sb=New-Object System.Text.StringBuilder 256; ' ..
        '[Diag.Win32]::GetWindowText($h,$sb,256) | Out-Null; $sb.ToString()"')
    out = out:gsub("%s+$", "")
    homrec.print(out ~= "" and ("Active window: " .. out) or "(couldn't read active window)")
end)
