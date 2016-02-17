package resolve

import (
	"fmt"
	"os/user"
	"path"

	"github.com/opentable/sous/tools/cli"
)

func Resolve(pathFormat string, a ...interface{}) string {
	p := fmt.Sprintf(pathFormat, a...)
	if p[:2] != "~/" {
		return p
	}
	u, err := user.Current()
	if err != nil {
		cli.Fatalf("unable to resolve path beginning ~/; %s", err)
	}
	return path.Join(u.HomeDir, p[2:])
}

func Dir(p string) string {
	return path.Dir(p)
}
