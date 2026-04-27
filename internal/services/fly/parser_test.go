package fly

import "testing"

func TestParseImageRef(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "single line with image",
			input: "image: registry.fly.io/wemob-acme:deployment-01HXYZ",
			want:  "registry.fly.io/wemob-acme:deployment-01HXYZ",
		},
		{
			name: "multi-line, last match wins",
			input: `==> Building image
image: registry.fly.io/wemob-acme:builder
image: registry.fly.io/wemob-acme:deployment-01HXYZ pushed
done`,
			want: "registry.fly.io/wemob-acme:deployment-01HXYZ",
		},
		{
			name:    "no image ref present",
			input:   "==> deploy failed",
			wantErr: true,
		},
		{
			name:  "image with dotted label",
			input: "registry.fly.io/wemob-foo:v1.2.3",
			want:  "registry.fly.io/wemob-foo:v1.2.3",
		},
	}
	for _, c := range cases {
		got, err := parseImageRef(c.input)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: error = %v, wantErr = %v", c.name, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
