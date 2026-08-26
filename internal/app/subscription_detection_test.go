package app

import "testing"

func TestDetectRegistrationType(t *testing.T) {
	tests := []struct {
		name     string
		section  string
		wantType string
		wantHost string
	}{
		{
			name:     "CDN with plain value",
			section:  "   hostname = rhsm.redhat.com\n   prefix = /subscription",
			wantType: "Red Hat CDN",
			wantHost: "rhsm.redhat.com",
		},
		{
			name:     "CDN with subdomain",
			section:  "   hostname = subscription.rhsm.redhat.com",
			wantType: "Red Hat CDN",
			wantHost: "subscription.rhsm.redhat.com",
		},
		{
			name:     "CDN with bracketed value",
			section:  "   hostname = [rhsm.redhat.com]\n   prefix = [/subscription]",
			wantType: "Red Hat CDN",
			wantHost: "rhsm.redhat.com",
		},
		{
			name:     "Satellite with custom hostname",
			section:  "   hostname = flxsatprd01.central.fluxys.int\n   prefix = /rhsm",
			wantType: "Satellite",
			wantHost: "flxsatprd01.central.fluxys.int",
		},
		{
			name:     "Satellite with satellite in hostname",
			section:  "   hostname = satellite.example.com",
			wantType: "Satellite",
			wantHost: "satellite.example.com",
		},
		{
			name:     "empty section",
			section:  "",
			wantType: "Unknown",
			wantHost: "",
		},
		{
			name:     "no hostname line",
			section:  "   prefix = /subscription\n   port = 443",
			wantType: "Unknown",
			wantHost: "",
		},
		{
			name:     "hostname with empty value",
			section:  "   hostname = []",
			wantType: "Unknown",
			wantHost: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotHost := detectRegistrationType(tt.section)
			if gotType != tt.wantType {
				t.Errorf("regType = %q, want %q", gotType, tt.wantType)
			}
			if gotHost != tt.wantHost {
				t.Errorf("serverHost = %q, want %q", gotHost, tt.wantHost)
			}
		})
	}
}
