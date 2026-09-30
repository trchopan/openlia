package cli

import "runtime/debug"

type versionInfo struct {
	Version  string
	Release  string
	Revision string
	Modified bool
	Hermes   string
	Locho    string
}

func currentVersionInfo() versionInfo {
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return versionInfo{Version: defaultVersion, Release: defaultVersion + "+unknown", Modified: true, Hermes: defaultHermesTag, Locho: defaultLochoVersion}
	}
	return versionInfoFromSettings(build.Settings)
}

func versionInfoFromSettings(settings []debug.BuildSetting) versionInfo {
	info := versionInfo{
		Version:  defaultVersion,
		Hermes:   defaultHermesTag,
		Locho:    defaultLochoVersion,
		Modified: true,
	}
	values := make(map[string]string, len(settings))
	for _, setting := range settings {
		values[setting.Key] = setting.Value
	}
	info.Revision = values["vcs.revision"]
	info.Modified = values["vcs.modified"] != "false"
	if len(info.Revision) < 12 {
		info.Release = defaultVersion + "+unknown"
		return info
	}
	if info.Modified {
		info.Release = defaultVersion + "+dev." + info.Revision[:12] + ".dirty"
	} else {
		info.Release = defaultVersion + "+release." + info.Revision[:12]
	}
	return info
}

func (info versionInfo) json() map[string]any {
	return map[string]any{
		"schema":   1,
		"version":  info.Version,
		"release":  info.Release,
		"revision": info.Revision,
		"modified": info.Modified,
		"hermes":   info.Hermes,
		"locho":    info.Locho,
	}
}

func (info versionInfo) human() string {
	return "openlia " + info.Release + " (Hermes " + info.Hermes + ", Locho " + info.Locho + ")"
}
