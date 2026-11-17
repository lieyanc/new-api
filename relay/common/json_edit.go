package common

import (
	"fmt"

	"github.com/MoeForge/moego/ojson"
)

// DeleteJSONMember removes every copy of the member at path. A client body
// may repeat a key, and most upstream parsers read the last copy, so a
// surviving copy could reach the upstream after the gateway removed or
// replaced the first one.
func DeleteJSONMember(doc *ojson.Doc, path string) error {
	for doc.Get(path).Exists() {
		size := doc.Len()
		if err := doc.Delete(path); err != nil {
			return err
		}
		if doc.Len() == size {
			return fmt.Errorf("delete %s: member was not removed", path)
		}
	}
	return nil
}
