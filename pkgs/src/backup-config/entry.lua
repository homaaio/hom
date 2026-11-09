-- backup-config/entry.lua
--
-- Cheap insurance for whenever you're about to go fiddle with settings
-- (or hand-edit an .hrc, or try a friend's exported config): snapshots
-- the current homrec.hrc into a timestamped backups\ folder, and can
-- restore one back. Plain file copy via io - homrec.* has no dedicated
-- backup/restore call, but it doesn't need one since .hrc is already
-- just a plain text file next to the exe (see hrc_config.cpp).

local BACKUP_DIR = "plugins\\backup-config-src\\backups"
local HRC_PATH = "homrec.hrc" -- HrcConfig::kDefaultSettingsPath, see hrc_config.cpp

local function copy_file(src, dest)
    local inf = io.open(src, "rb")
    if not inf then return false, "can't open " .. src end
    local data = inf:read("*a")
    inf:close()
    local outf = io.open(dest, "wb")
    if not outf then return false, "can't write " .. dest end
    outf:write(data)
    outf:close()
    return true
end

homrec.register_command("cfgbackup", "cfgbackup save|list|restore [name]", function(raw)
    local _, rest = raw:match("^(%S*)%s*(.-)$")
    local sub, name = rest:match("^(%S*)%s*(.-)$")

    os.execute('mkdir "' .. BACKUP_DIR .. '" 2> nul')

    if sub == "save" then
        local stamp = name ~= "" and name or os.date("%Y%m%d_%H%M%S")
        local dest = BACKUP_DIR .. "\\" .. stamp .. ".hrc"
        local ok, err = copy_file(HRC_PATH, dest)
        if ok then homrec.print("Backed up to " .. dest)
        else homrec.print("Backup failed: " .. tostring(err)) end

    elseif sub == "list" then
        local p = io.popen('dir /b "' .. BACKUP_DIR .. '\\*.hrc" 2> nul')
        local out = p and p:read("*a") or ""
        if p then p:close() end
        if out == "" then
            homrec.print("No backups yet - cfgbackup save")
        else
            for line in out:gmatch("[^\r\n]+") do
                homrec.print("  " .. line:gsub("%.hrc$", ""))
            end
        end

    elseif sub == "restore" then
        if name == "" then homrec.print("Usage: cfgbackup restore <name>"); return end
        local src = BACKUP_DIR .. "\\" .. name .. ".hrc"
        local ok, err = copy_file(src, HRC_PATH)
        if ok then
            homrec.print("Restored " .. name .. " - restart HomRec (or File > Load Config) to apply it.")
        else
            homrec.print("Restore failed: " .. tostring(err))
        end

    else
        homrec.print("Usage: cfgbackup save|list|restore [name]")
    end
end)
