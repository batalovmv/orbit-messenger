package config

import (
	"testing"
)

func TestParsePostgresURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "special chars in password",
			url:  `postgres://myuser:dM[]C?a^4fG0|tJ?amd.ZAaIgh\/yViO@myhost:5432/mydb`,
			want: `host=myhost port=5432 user=myuser password='dM[]C?a^4fG0|tJ?amd.ZAaIgh\\/yViO' dbname=mydb sslmode=disable`,
		},
		{
			name: "simple password",
			url:  "postgres://user:pass@host:5432/db",
			want: "host=host port=5432 user=user password='pass' dbname=db sslmode=disable",
		},
		{
			name: "password with @",
			url:  "postgres://user:p@ss@host:5432/db",
			want: "host=host port=5432 user=user password='p@ss' dbname=db sslmode=disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePostgresURL(tt.url)
			if got != tt.want {
				t.Errorf("parsePostgresURL()\n got  = %s\n want = %s", got, tt.want)
			}
		})
	}
}
