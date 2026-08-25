// Command runid creates the short UUIDv7 identifier used to name one platform run.
package main

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func main() {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	fmt.Println(strings.ReplaceAll(id.String(), "-", "")[:12])
}
