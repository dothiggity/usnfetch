package app

import "testing"

// A saved provider password is only ever sent to the server it was saved for: an edit (or a
// connection test) that names another server without typing the password gets none.
func TestMergeAccountsPassword(t *testing.T) {
	m, err := NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Store.Close)
	m.Settings.Accounts = []AccountSetting{{ID: "a1", Server: "news.example.com", Port: 563, Username: "me", Password: "secret"}}
	merge := func(in map[string]any) string {
		out := m.MergeAccounts([]map[string]any{in})
		if len(out) != 1 {
			t.Fatalf("%v: %d accounts", in, len(out))
		}
		return out[0].Password
	}
	for _, x := range []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"id": "a1", "server": "news.example.com", "port": 119.0}, "secret"},         // same server, other port: kept
		{map[string]any{"id": "a1", "server": "NEWS.example.com"}, "secret"},                        // same name, other case
		{map[string]any{"id": "a1", "server": "attacker.example", "password": ""}, ""},              // another server: not sent there
		{map[string]any{"id": "a1", "server": "secure.news.example.com"}, ""},                       // even the same provider's other name
		{map[string]any{"id": "a1", "server": "secure.news.example.com", "password": "new"}, "new"}, // typed again
		{map[string]any{"id": "a1", "server": "news.example.com", "clear_password": true}, ""},      // cleared
		{map[string]any{"id": "zz", "server": "news.example.com"}, ""},                              // another account
	} {
		if got := merge(x.in); got != x.want {
			t.Errorf("%v: password %q, want %q", x.in, got, x.want)
		}
	}
}
