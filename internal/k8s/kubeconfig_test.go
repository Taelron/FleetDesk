package k8s

import "testing"

func TestParseContextLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want K8sContext
	}{
		{
			name: "regular context",
			line: "      AKS-APP-DEV-BLUE                         AKS-APP-DEV-BLUE       clusterUser_RG-APP-DEV_AKS-APP-DEV-BLUE",
			want: K8sContext{Name: "AKS-APP-DEV-BLUE", Cluster: "AKS-APP-DEV-BLUE", User: "clusterUser_RG-APP-DEV_AKS-APP-DEV-BLUE"},
		},
		{
			name: "current context with namespace",
			line: "*     AKS-MAN-DEV-GREEN-admin                  AKS-MAN-DEV-GREEN      clusterAdmin_RG-MAN-DEV_AKS-MAN-DEV-GREEN   argocd",
			want: K8sContext{Name: "AKS-MAN-DEV-GREEN-admin", Cluster: "AKS-MAN-DEV-GREEN", User: "clusterAdmin_RG-MAN-DEV_AKS-MAN-DEV-GREEN", Namespace: "argocd", Current: true},
		},
		{
			name: "empty line",
			line: "",
			want: K8sContext{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseContextLine(tt.line)
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Cluster != tt.want.Cluster {
				t.Errorf("Cluster = %q, want %q", got.Cluster, tt.want.Cluster)
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.Namespace != tt.want.Namespace {
				t.Errorf("Namespace = %q, want %q", got.Namespace, tt.want.Namespace)
			}
			if got.Current != tt.want.Current {
				t.Errorf("Current = %v, want %v", got.Current, tt.want.Current)
			}
		})
	}
}

func TestParseK8sListCount(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{"3 items", []byte(`{"items":[{},{},{}]}`), 3},
		{"empty list", []byte(`{"items":[]}`), 0},
		{"nil", nil, 0},
		{"invalid", []byte("not json"), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseK8sListCount(tt.input)
			if got != tt.want {
				t.Errorf("parseK8sListCount() = %d, want %d", got, tt.want)
			}
		})
	}
}
