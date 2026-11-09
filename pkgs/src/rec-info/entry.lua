-- rec-info/entry.lua
--
-- "recinfo" console command: prints the current recording configuration
-- in one place (resolution/fps/codec/quality/hw accel/mic/output folder).
-- Not a LIVE stats overlay (dropped frames, actual encode fps while
-- recording) - there's no exposed getter for that, homrec.settings_get()
-- only reads the *configured* .hrc fields (see HrSettingsRegistry), not
-- the live pipeline counters main_frame.cpp's own stats tick uses
-- internally. Still genuinely useful as a fast "what am I about to
-- record with" sanity check before hitting Start.

local FIELDS = {
    { key = "resolution_w",      label = "Width" },
    { key = "resolution_h",      label = "Height" },
    { key = "target_fps",        label = "FPS" },
    { key = "video_codec",       label = "Codec" },
    { key = "quality",           label = "Quality" },
    { key = "enc_crf",           label = "CRF" },
    { key = "enc_preset",        label = "Encoder preset" },
    { key = "hw_accel",          label = "HW accel" },
    { key = "audio_aac_bitrate", label = "Audio bitrate" },
    { key = "mic_device_id",     label = "Mic device" },
    { key = "output_folder",     label = "Output folder" },
    { key = "instant_replay_enabled", label = "Instant Replay" },
    { key = "replay_buffer_sec", label = "Replay buffer (s)" },
}

homrec.register_command("recinfo", "Print the current recording configuration", function(raw)
    homrec.print("-- HomRec recording config --")
    for _, f in ipairs(FIELDS) do
        local v = homrec.settings_get(f.key)
        homrec.print(string.format("%-16s %s", f.label .. ":", tostring(v)))
    end
end)
