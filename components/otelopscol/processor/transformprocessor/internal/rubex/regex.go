package rubex

/*
#include <stdlib.h>
#include "oniguruma.h"
#include "chelper.h"
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

const numMatchStartSize = 4
const numReadBufferStartSize = 256

var mutex sync.Mutex

type NamedGroupInfo map[string]int

type Regexp struct {
	pattern   string
	regex     C.OnigRegex
	encoding  C.OnigEncoding
	errorInfo *C.OnigErrorInfo
	errorBuf  *C.char

	numCaptures        int32
	namedGroupInfo     NamedGroupInfo
	orderedNamedGroups []string
}

// NewRegexp creates and initializes a new Regexp with the given pattern and option.
func NewRegexp(pattern string, option int) (*Regexp, error) {
	return initRegexp(&Regexp{pattern: pattern, encoding: C.ONIG_ENCODING_UTF8}, option)
}

func initRegexp(re *Regexp, option int) (*Regexp, error) {
	patternCharPtr := C.CString(re.pattern)
	defer C.free(unsafe.Pointer(patternCharPtr))

	mutex.Lock()
	defer mutex.Unlock()

	errorCode := C.NewOnigRegex(patternCharPtr, C.int(len(re.pattern)), C.int(option), &re.regex, &re.encoding, &re.errorInfo, &re.errorBuf)
	if errorCode != C.ONIG_NORMAL {
		return re, errors.New(C.GoString(re.errorBuf))
	}

	re.numCaptures = int32(C.onig_number_of_captures(re.regex)) + 1
	re.namedGroupInfo = re.getNamedGroupInfo()

	runtime.SetFinalizer(re, (*Regexp).Free)

	return re, nil
}

func Compile(str string) (*Regexp, error) {
	return NewRegexp(str, ONIG_OPTION_DEFAULT)
}

func MustCompile(str string) *Regexp {
	regexp, error := NewRegexp(str, ONIG_OPTION_DEFAULT)
	if error != nil {
		panic("regexp: compiling " + str + ": " + error.Error())
	}

	return regexp
}

func (re *Regexp) Free() {
	mutex.Lock()
	if re.regex != nil {
		C.onig_free(re.regex)
		re.regex = nil
	}
	mutex.Unlock()
	if re.errorInfo != nil {
		C.free(unsafe.Pointer(re.errorInfo))
		re.errorInfo = nil
	}
	if re.errorBuf != nil {
		C.free(unsafe.Pointer(re.errorBuf))
		re.errorBuf = nil
	}
}

func (re *Regexp) SubexpNames() []string {
	return re.orderedNamedGroups
}

func (re *Regexp) getNamedGroupInfo() NamedGroupInfo {
	numNamedGroups := int(C.onig_number_of_names(re.regex))
	// when any named capture exists, there is no numbered capture even if
	// there are unnamed captures.
	if numNamedGroups == 0 {
		return nil
	}

	namedGroupInfo := make(map[string]int)

	//try to get the names
	bufferSize := len(re.pattern) * 2
	nameBuffer := make([]byte, bufferSize)
	groupNumbers := make([]int32, numNamedGroups)
	bufferPtr := unsafe.Pointer(&nameBuffer[0])
	numbersPtr := unsafe.Pointer(&groupNumbers[0])

	length := int(C.GetCaptureNames(re.regex, bufferPtr, (C.int)(bufferSize), (*C.int)(numbersPtr)))
	if length == 0 {
		panic(fmt.Errorf("could not get the capture group names from %q", re.String()))
	}

	namesAsBytes := bytes.Split(nameBuffer[:length], ([]byte)(";"))
	if len(namesAsBytes) != numNamedGroups {
		panic(fmt.Errorf(
			"the number of named groups (%d) does not match the number names found (%d)",
			numNamedGroups, len(namesAsBytes),
		))
	}

	re.orderedNamedGroups = make([]string, length)
	for i, nameAsBytes := range namesAsBytes {
		name := string(nameAsBytes)
		namedGroupInfo[name] = int(groupNumbers[i])
		re.orderedNamedGroups[namedGroupInfo[name]-1] = name
	}

	return namedGroupInfo
}

func (re *Regexp) find(b []byte, n int, offset int) []int {
	match := make([]int, re.numCaptures*2)

	if n == 0 {
		b = []byte{0}
	}

	bytesPtr := unsafe.Pointer(&b[0])

	// captures contains two pairs of ints, start and end, so we need list
	// twice the size of the capture groups.
	captures := make([]C.int, re.numCaptures*2)
	capturesPtr := unsafe.Pointer(&captures[0])

	var numCaptures int32
	numCapturesPtr := unsafe.Pointer(&numCaptures)

	pos := int(C.SearchOnigRegex(
		bytesPtr, C.int(n), C.int(offset), C.int(ONIG_OPTION_DEFAULT),
		re.regex, re.errorInfo, (*C.char)(nil), (*C.int)(capturesPtr), (*C.int)(numCapturesPtr),
	))

	if pos < 0 {
		return nil
	}

	if numCaptures <= 0 {
		panic("cannot have 0 captures when processing a match")
	}

	if re.numCaptures != numCaptures {
		panic(fmt.Errorf("expected %d captures but got %d", re.numCaptures, numCaptures))
	}

	for i := range captures {
		match[i] = int(captures[i])
	}

	return match
}

func getCapture(b []byte, beg int, end int) []byte {
	if beg < 0 || end < 0 {
		return nil
	}

	return b[beg:end]
}

func (re *Regexp) match(b []byte, n int, offset int) bool {
	if n == 0 {
		b = []byte{0}
	}

	bytesPtr := unsafe.Pointer(&b[0])
	pos := int(C.SearchOnigRegex(
		bytesPtr, C.int(n), C.int(offset), C.int(ONIG_OPTION_DEFAULT),
		re.regex, re.errorInfo, nil, nil, nil,
	))

	return pos >= 0
}

func (re *Regexp) FindSubmatchIndex(b []byte) []int {
	match := re.find(b, len(b), 0)
	if len(match) == 0 {
		return nil
	}

	return match
}

func (re *Regexp) FindStringSubmatch(s string) []string {
	b := []byte(s)
	match := re.FindSubmatchIndex(b)
	if match == nil {
		return nil
	}

	length := len(match) / 2
	if length == 0 {
		return nil
	}

	results := make([]string, 0, length)
	for i := 0; i < length; i++ {
		cap := getCapture(b, match[2*i], match[2*i+1])
		if cap == nil {
			results = append(results, "")
		} else {
			results = append(results, string(cap))
		}
	}

	return results
}

func (re *Regexp) Match(b []byte) bool {
	return re.match(b, len(b), 0)
}

func (re *Regexp) MatchString(s string) bool {
	return re.Match([]byte(s))
}

func (re *Regexp) NumSubexp() int {
	return (int)(C.onig_number_of_captures(re.regex))
}

func (re *Regexp) String() string {
	return re.pattern
}
