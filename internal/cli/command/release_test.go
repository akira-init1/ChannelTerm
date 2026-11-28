package command

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestReleaseHelpAndNoninteractiveValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "/dev/cu."}, {[]string{"COM8"}, "--yes"}, {[]string{"--all"}, "--yes"},
		{[]string{"COM8", "--all", "--yes"}, "specify"}, {[]string{"--yes"}, "specify"}, {[]string{"one", "two"}, "expected"},
	} {
		var out bytes.Buffer
		err := runRelease(context.Background(), tc.args, strings.NewReader("yes\n"), &out)
		if tc.args[0] == "--help" {
			if err != nil || !strings.Contains(out.String(), tc.want) {
				t.Fatal(out.String(), err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatal(tc.args, err)
		}
	}
}
