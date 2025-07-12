package libfreetype

import "fmt"

const (
	MAJOR uint = 2
	MINOR uint = 13
	PATCH uint = 3
	BUILD uint = 0
)

func Version() string {
	return fmt.Sprintf("%d.%d.%d", MAJOR, MINOR, PATCH)
}

func VersionWithBuild() string {
	return fmt.Sprintf("%d.%d.%d-%d", MAJOR, MINOR, PATCH, BUILD)
}
