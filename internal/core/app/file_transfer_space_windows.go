package app

import (
	"math"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func localTransferStorage(path string) (transferStorage, error) {
	path, err := existingStoragePath(path)
	if err != nil {
		return transferStorage{}, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return transferStorage{}, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(name, &available, &total, &free); err != nil {
		return transferStorage{}, err
	}
	var volume [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumePathName(name, &volume[0], uint32(len(volume))); err != nil {
		return transferStorage{}, err
	}
	var sectors, bytes, freeClusters, clusters uint32
	// x/sys exposes the capacity query but not the allocation-unit query.
	query := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")
	ok, _, callErr := query.Call(uintptr(unsafe.Pointer(&volume[0])), uintptr(unsafe.Pointer(&sectors)), uintptr(unsafe.Pointer(&bytes)), uintptr(unsafe.Pointer(&freeClusters)), uintptr(unsafe.Pointer(&clusters)))
	if ok == 0 {
		return transferStorage{}, callErr
	}
	block := uint64(sectors) * uint64(bytes)
	if available > math.MaxInt64 || block == 0 || block > math.MaxInt64 {
		return transferStorage{}, ErrTransferSpaceUnavailable
	}
	return transferStorage{available: int64(available), block: int64(block), inodes: -1, volume: strings.ToLower(windows.UTF16ToString(volume[:]))}, nil
}
