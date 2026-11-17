package daemon

import (
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
)

// settings_adapter.go — S44 T5: the daemon's wiring of the settings seam.

// *config.Store already satisfies protocol.SettingsService field-for-field
// (the seam returns config's own View/Event; the protocol handler maps them
// to its wire twins, protocol/settingView). The adapter is therefore a nil
// guard, not a translation — it mirrors the newTelemetryReadStore convention
// (internal/daemon/telemetry_reads.go): a storeless daemon (V13) keeps nil,
// and RegisterSettings leaves settings.* ABSENT rather than refusing. A
// second value type here would be a second seam; this is the one.
var _ protocol.SettingsService = (*config.Store)(nil)

// settingsService maps the daemon's ONE shared *config.Store (opts.Settings,
// built once at the entry point by NewSettings, T4) onto the protocol's
// settings seam. It is the nil guard the newTelemetryReadStore convention
// gives every store-backed seam: a storeless daemon keeps nil, so
// RegisterSettings is a no-op and settings.* stays absent (V13), rather than
// the daemon answering a settings door it cannot serve.
func settingsService(s *config.Store) protocol.SettingsService {
	if s == nil {
		return nil
	}
	return s
}
