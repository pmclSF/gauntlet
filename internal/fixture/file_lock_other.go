//go:build !unix

package fixture

func lockPath(path string) (func(), error) {
	return func() {}, nil
}
