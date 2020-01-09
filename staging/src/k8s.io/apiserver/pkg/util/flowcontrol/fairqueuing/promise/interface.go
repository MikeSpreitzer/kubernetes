/*
Copyright 2019 The Kubernetes Authors.

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

package promise

// Readable is a variable that is initially not set and later becomes
// set.  Some instances may be set to multiple values in series.
type Readable interface {
	// Get reads the current value of this variable.  If this variable
	// is not set yet then this call blocks until this variable gets a
	// value.
	Get() interface{}
}

// ReadableLocked is a Readable whose implementation is protected by a lock
type LockingReadable interface {
	Readable

	// GetLocked is like Get but the caller must already hold the lock
	GetLocked() interface{}
}

// WriteOnlyOnce is a variable that is initially not set and can be set once.
type WriteOnlyOnce interface {
	// Set writes a value into this variable and unblocks every
	// goroutine waiting for this variable to have a value
	Set(interface{})
}

// WriteOnce is a variable that is initially not set and can be set once.
type WriteOnce interface {
	Readable
	WriteOnlyOnce
}

// LockingWriteOnce is a Mutable whose implementation is protected by a lock
type LockingWriteOnce interface {
	LockingReadable
	WriteOnlyOnce

	// SetLocked is like Set but the caller must already hold the lock
	SetLocked(interface{})
}

// WriteOnlyMultiple is a variable that is initially not set and can be set one
// or more times (unlike a traditional "promise", which can be written
// only once).
type WriteOnlyMultiple interface {
	// Set writes a value into this variable and unblocks every
	// goroutine waiting for this variable to have a value
	Set(interface{})
}

// Mutable is a variable that is initially not set and can be set one
// or more times (unlike a traditional "promise", which can be written
// only once).
type Mutable interface {
	Readable
	WriteOnlyMultiple
}

// LockingMutable is a Mutable whose implementation is protected by a lock
type LockingMutable interface {
	LockingReadable
	WriteOnlyMultiple

	// SetLocked is like Set but the caller must already hold the lock
	SetLocked(interface{})
}
