/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

const (
	replacementPrefix         = "replacement-at-"
	creatingReplacementPrefix = "creating-replacement-at-"
	initialDirName            = "initial"
	writeDirName              = "writeTemp"
)

// mapInFilesystem implements Map by storage in the local filesystem.
// For values it supports only those that a given Codec can [de]serialize.
// The storage is in a filesystem branch whose pathname is in `head`.
// The head is a directory containing the following things.
//   - `replacement-at-${timestamp}`: directory containing replacement done at the indicated time;
//     absent if Replace has not yet been called.
//   - `creating-replacement-at-${timestamp}`: replacement directory in the process of
//     being populated.
//   - `initial`: directory containing entries written before first call to Replace;
//     absent after Replace has been called.
//   - `writeTemp`: the subdirectory where entries are written non-atomically.
//   - an entry whose name begins with a dot is insignificant here.
//
// Among the initial and replacements subdirectories, the one with the lexicographic highest
// name holds the current contents and any others that exist are junk to be deleted.
//
// In the directory holding the current map contents, there is a file for every (key,value) pair
// in the map.  There is a 1:1 mapping between key space and filesystem-safe filenames.
type mapInFilesystem struct {
	// name distinguishes instances in log messages
	name string

	// head is absolute path to head directory
	head string

	currentContentPath string

	codec k8sruntime.Codec
}

func NewMapinFilesystem(name, head string, codec k8sruntime.Codec) (Map, error) {
	absHead, err := filepath.Abs(head)
	if err != nil {
		return nil, err
	}
	mif := &mapInFilesystem{
		name:  name,
		head:  absHead,
		codec: codec,
	}
	err = mif.initialize()
	if err != nil {
		return nil, err
	}
	return mif, nil
}

func (mif *mapInFilesystem) initialize() error {
	writePath := filepath.Join(mif.head, writeDirName)
	headStat, err := os.Stat(mif.head)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		err = os.MkdirAll(mif.head, 0750)
		if err != nil {
			return err
		}
		klog.V(5).InfoS("Created head directory for map", "mapName", mif.name, "head", mif.head)
		err = os.Mkdir(writePath, 0750)
		if err != nil {
			return err
		}
		initialPath := filepath.Join(mif.head, initialDirName)
		err = os.Mkdir(initialPath, 0750)
		if err != nil {
			return err
		}
		mif.currentContentPath = initialPath
		return nil
	}
	if !headStat.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", mif.head)
	}
	klog.V(5).InfoS("Using existing head directory for map", "mapName", mif.name, "head", mif.head)
	mif.ensureDirectory(writePath)
	goners := []fs.DirEntry{}
	headEntries, err := os.ReadDir(mif.head)
	if err != nil {
		return err
	}
	var latestContent fs.DirEntry
	for _, entry := range headEntries {
		entryName := entry.Name()
		if len(entryName) > 0 && entryName[0] == '.' {
			continue
		}
		if entryName == writeDirName {
			continue
		}
		if (entryName == initialDirName || strings.HasPrefix(entryName, replacementPrefix)) && (latestContent == nil || entryName > latestContent.Name()) {
			if latestContent != nil {
				goners = append(goners, latestContent)
			}
			latestContent = entry
			continue
		}
		goners = append(goners, entry)
	}
	if latestContent == nil {
		initialPath := filepath.Join(mif.head, initialDirName)
		err = mif.ensureDirectory(initialPath)
		if err != nil {
			return err
		}
		mif.currentContentPath = initialPath
	} else {
		mif.currentContentPath = filepath.Join(mif.head, latestContent.Name())
		klog.V(5).InfoS("Using existing content directory for map", "mapName", mif.name, "currentContentPath", mif.currentContentPath)
	}
	for _, entry := range goners {
		entryPath := filepath.Join(mif.head, entry.Name())
		klog.V(2).InfoS("Removing junk entry from map head", "mapName", mif.name, "entryPath", entryPath, "isDir", entry.IsDir())
		if entry.IsDir() {
			err = os.RemoveAll(entryPath)
			if err != nil {
				return err
			}
		} else {
			err = os.Remove(entryPath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (mif *mapInFilesystem) ensureDirectory(dirPath string) error {
	writeInfo, err := os.Stat(dirPath)
	if errors.Is(err, fs.ErrNotExist) {
		klog.V(5).InfoS("Creating subdirectory for map", "mapName", mif.name, "dirPath", dirPath)
		return os.Mkdir(dirPath, 0750)
	}
	if err != nil {
		return err
	}
	if writeInfo.IsDir() {
		klog.V(5).InfoS("Using existing subdirectory for map", "mapName", mif.name, "dirPath", dirPath)
		return nil
	}
	klog.V(5).InfoS("Replacing junk with subdirectory for map", "mapName", mif.name, "dirPath", dirPath)
	err = os.Remove(dirPath)
	if err != nil {
		return err
	}
	return os.Mkdir(dirPath, 0750)
}

func (mif *mapInFilesystem) Get(key string) (interface{}, bool) {
	filename := keyToFilename(key)
	path := filepath.Join(mif.currentContentPath, filename)
	bytes, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			klog.V(5).InfoS("Get returns nothing because ReadFile errored", "mapName", mif.name, "key", key, "err", err)
		}
		return nil, false
	}
	obj, err := k8sruntime.Decode(mif.codec, bytes)
	if err != nil {
		klog.ErrorS(err, "Failed to decode object read from filesystem", "path", path)
		return nil, false
	}
	klog.V(8).InfoS("Successful Get", "mapName", mif.name, "key", key, "byteLen", len(bytes))
	return obj, true
}

func (mif *mapInFilesystem) Put(key string, objAny interface{}) {
	mif.writeObjAny(mif.currentContentPath, key, objAny)
}

func (mif *mapInFilesystem) writeObjAny(saveDir, key string, objAny interface{}) {
	obj, ok := objAny.(k8sruntime.Object)
	if !ok {
		klog.ErrorS(nil, "Told to Put a value of unsupported type", "mapName", mif.name, "key", key, "value", fmt.Sprintf("%#+v", objAny), "type", fmt.Sprintf("%T", objAny))
		return
	}
	bytes, err := k8sruntime.Encode(mif.codec, obj)
	if err != nil {
		klog.ErrorS(err, "Failed to encode object for Put", "mapName", mif.name, "key", key, "value", fmt.Sprintf("%#+v", objAny), "type", fmt.Sprintf("%T", objAny))
		return
	}
	filename := keyToFilename(key)
	writePath := filepath.Join(mif.head, writeDirName, filename)
	file, err := os.Create(writePath)
	if err != nil {
		klog.ErrorS(err, "Failed to create file for Put", "mapName", mif.name, "key", key, "writePath", writePath)
		return
	}
	_, err = file.Write(bytes)
	if err != nil {
		klog.ErrorS(err, "Failed to write file for Put", "mapName", mif.name, "key", key, "writePath", writePath)
		return
	}
	err = file.Close()
	if err != nil {
		klog.ErrorS(err, "Failed to close file for Put", "mapName", mif.name, "key", key, "writePath", writePath)
		return
	}
	savePath := filepath.Join(saveDir, filename)
	err = os.Rename(writePath, savePath)
	if err != nil {
		klog.ErrorS(err, "Failed to rename file for Put", "mapName", mif.name, "key", key, "writePath", writePath, "savePath", savePath)
	} else {
		klog.V(8).InfoS("Successful write", "mapName", mif.name, "key", key, "byteLen", len(bytes))
	}
}

func (mif *mapInFilesystem) Replace(replacement map[string]interface{}) {
	suffix := time.Now().Format("2006-01-02T15-04-05.999999999Z07-00")
	creatingReplacementPath := filepath.Join(mif.head, creatingReplacementPrefix+suffix)
	replacementPath := filepath.Join(mif.head, replacementPrefix+suffix)
	err := os.Mkdir(creatingReplacementPath, 0750)
	if err != nil {
		klog.ErrorS(err, "Failed to create temporary replacement directory", "mapName", mif.name, "creatingReplacementPath", creatingReplacementPath)
		return
	}
	for key, objAny := range replacement {
		mif.writeObjAny(creatingReplacementPath, key, objAny)
	}
	err = os.Rename(creatingReplacementPath, replacementPath)
	if err != nil {
		klog.ErrorS(err, "Failed to rename new replacement directory", "mapName", mif.name, "oldPath", creatingReplacementPath, "newPath", replacementPath)
		return
	}
	klog.V(8).InfoS("Successful replace", "mapName", mif.name, "replacementPath", replacementPath)
	prevCurrent := mif.currentContentPath
	mif.currentContentPath = replacementPath
	err = os.RemoveAll(prevCurrent)
	if err != nil {
		klog.Error(err, "Failed to remove previous contents", "mapName", mif.name, "prevCurrent", prevCurrent)
	}
}

func (mif *mapInFilesystem) CheapLengthEstimate() int {
	return 8
}

func (mif *mapInFilesystem) Delete(key string) {
	filename := keyToFilename(key)
	path := filepath.Join(mif.currentContentPath, filename)
	err := os.Remove(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			klog.V(5).InfoS("Delete got error from Remove", "mapName", mif.name, "key", key, "err", err)
		} else {
			klog.V(8).InfoS("Already gone", "mapName", mif.name, "key", key)
		}
		return
	}
	klog.V(8).InfoS("Successful Delete", "mapName", mif.name, "key", key)
}

func (mif *mapInFilesystem) IsEmpty() bool {
	ans := true
	mif.Enumerate(func(string, interface{}) error {
		ans = false
		return io.EOF
	})
	return ans
}

func (mif *mapInFilesystem) Enumerate(visitor func(string, interface{}) error) error {
	dirEntries, err := os.ReadDir(mif.currentContentPath)
	if err != nil {
		klog.V(5).ErrorS(err, "Unable to read current contents directory", "mapName", mif.name, "currentContentPath", mif.currentContentPath)
	}
	for _, dirEntry := range dirEntries {
		filename := dirEntry.Name()
		if len(filename) > 0 && filename[0] == '.' {
			continue
		}
		key := filenameToKey(mif.name, filename)
		path := filepath.Join(mif.currentContentPath, filename)
		bytes, err := os.ReadFile(path)
		if err != nil {
			klog.V(5).InfoS("Read of directory entry fails", "mapName", mif.name, "path", path, "err", err)
		} else {
			obj, err := k8sruntime.Decode(mif.codec, bytes)
			if err != nil {
				klog.ErrorS(err, "Failed to decode object read from filesystem", "mapName", mif.name, "path", path)
			} else {
				err = visitor(key, obj)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func keyToFilename(key string) string {
	if key == "." {
		return "@2E"
	}
	if key == ".." {
		return "@2E@2E"
	}
	var bld strings.Builder
	for idx, ch := range key {
		switch {
		case '0' <= ch && ch <= '9', 'a' <= ch && ch <= 'z', 'A' <= ch && ch <= 'Z', ch == '.' && idx > 0, ch == '-', ch == '_':
			bld.WriteRune(ch)
		case ch == '/':
			bld.WriteRune('%')
		case ch < 256:
			bld.WriteRune('@')
			bld.WriteRune(hexDigits[ch>>4])
			bld.WriteRune(hexDigits[ch%16])
		default:
			ch32 := uint32(ch)
			bld.WriteRune('#')
			bld.WriteRune(hexDigits[ch32>>28])
			bld.WriteRune(hexDigits[(ch32>>24)%16])
			bld.WriteRune(hexDigits[(ch32>>20)%16])
			bld.WriteRune(hexDigits[(ch32>>16)%16])
			bld.WriteRune(hexDigits[(ch32>>12)%16])
			bld.WriteRune(hexDigits[(ch32>>8)%16])
			bld.WriteRune(hexDigits[(ch32>>4)%16])
			bld.WriteRune(hexDigits[ch32%16])
		}
	}
	return bld.String()
}

func filenameToKey(mapName, filename string) string {
	var bld strings.Builder
	var i int
	for i < len(filename) {
		ch := filename[i]
		switch {
		case '0' <= ch && ch <= '9', 'a' <= ch && ch <= 'z', 'A' <= ch && ch <= 'Z', ch == '.', ch == '-', ch == '_':
			bld.WriteByte(ch)
			i += 1
		case ch == '%':
			bld.WriteRune('/')
			i += 1
		case ch == '@':
			if i+2 >= len(filename) {
				klog.ErrorS(nil, "Truncated short hex escape", "mapName", mapName, "filename", filename)
				break
			}
			d0, ok0 := unhex[filename[i+1]]
			d1, ok1 := unhex[filename[i+2]]
			if !(ok0 && ok1) {
				klog.ErrorS(nil, "Invalid short hex escape", "mapName", mapName, "filename", filename)
				bld.WriteRune('@')
				bld.WriteByte(filename[i+1])
				bld.WriteByte(filename[i+2])
			} else {
				ch8 := byte(d0*16 + d1)
				bld.WriteByte(ch8)
			}
			i += 3
		case ch == '#':
			if i+8 >= len(filename) {
				klog.ErrorS(nil, "Truncated long hex escape", "mapName", mapName, "filename", filename)
				break
			}
			d0, ok0 := unhex[filename[i+1]]
			d1, ok1 := unhex[filename[i+2]]
			d2, ok2 := unhex[filename[i+3]]
			d3, ok3 := unhex[filename[i+4]]
			d4, ok4 := unhex[filename[i+5]]
			d5, ok5 := unhex[filename[i+6]]
			d6, ok6 := unhex[filename[i+7]]
			d7, ok7 := unhex[filename[i+8]]
			if !(ok0 && ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) {
				klog.ErrorS(nil, "Invalid long hex escape", "mapName", mapName, "filename", filename)
				bld.WriteRune('@')
				bld.WriteByte(filename[i+1])
				bld.WriteByte(filename[i+2])
				bld.WriteByte(filename[i+3])
				bld.WriteByte(filename[i+4])
				bld.WriteByte(filename[i+5])
				bld.WriteByte(filename[i+6])
				bld.WriteByte(filename[i+7])
				bld.WriteByte(filename[i+8])
			} else {
				ch32 := rune(d0<<28 + d1<<24 + d2<<20 + d3<<16 + d4<<12 + d5<<8 + d6<<4 + d7)
				bld.WriteRune(ch32)
			}
			i += 9
		default:
			klog.ErrorS(nil, "Invalid character", "mapName", mapName, "filename", filename, "character", ch)
			bld.WriteByte(ch)
			i += 1
		}
	}
	return bld.String()
}

var hexDigits = []rune{'0', '1', '2', '3', '4', '5', '6', '7',
	'8', '9', 'A', 'B', 'C', 'D', 'E', 'F'}

var unhex = map[byte]uint32{
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7,
	'8': 8, '9': 9, 'A': 10, 'B': 11, 'C': 12, 'D': 13, 'E': 14, 'F': 15,
	'a': 10, 'b': 11, 'c': 12, 'd': 13, 'e': 14, 'f': 15,
}
