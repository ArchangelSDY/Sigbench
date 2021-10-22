package util

import "testing"

func TestGetVMSSInstanceId(t *testing.T) {
	t.Run("daysh85d200001D", func(t *testing.T) {
		if id, err := GetVMSSInstanceId("daysh85d200001D"); err == nil {
			if id != int64(49) {
				t.Fatal("Expect 49 but got", id)
			}
		} else {
			t.Fatal(err)
		}
	});
}
