package util

import (
	"strconv"
)

func GetVMSSInstanceId(hostname string) (int64, error) {
	idPart := hostname[len(hostname) - 6:len(hostname)]
	return strconv.ParseInt(idPart, 36, 64)
}
