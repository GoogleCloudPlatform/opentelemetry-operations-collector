/**********************************************************************
  constants.go -  Rubex (https://github.com/moovweb/rubex)
**********************************************************************/
/*-
 * Copyright (C) 2011 by Zhigang Chen

 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.
 */

package rubex

const (
	ONIG_OPTION_DEFAULT = ONIG_OPTION_NONE
	/* options */
	ONIG_OPTION_NONE               = 0
	ONIG_OPTION_IGNORECASE         = 1
	ONIG_OPTION_EXTEND             = (ONIG_OPTION_IGNORECASE << 1)
	ONIG_OPTION_MULTILINE          = (ONIG_OPTION_EXTEND << 1)
	ONIG_OPTION_SINGLELINE         = (ONIG_OPTION_MULTILINE << 1)
	ONIG_OPTION_FIND_LONGEST       = (ONIG_OPTION_SINGLELINE << 1)
	ONIG_OPTION_FIND_NOT_EMPTY     = (ONIG_OPTION_FIND_LONGEST << 1)
	ONIG_OPTION_NEGATE_SINGLELINE  = (ONIG_OPTION_FIND_NOT_EMPTY << 1)
	ONIG_OPTION_DONT_CAPTURE_GROUP = (ONIG_OPTION_NEGATE_SINGLELINE << 1)
	ONIG_OPTION_CAPTURE_GROUP      = (ONIG_OPTION_DONT_CAPTURE_GROUP << 1)
	/* options (search time) */
	ONIG_OPTION_NOTBOL       = (ONIG_OPTION_CAPTURE_GROUP << 1)
	ONIG_OPTION_NOTEOL       = (ONIG_OPTION_NOTBOL << 1)
	ONIG_OPTION_POSIX_REGION = (ONIG_OPTION_NOTEOL << 1)
	ONIG_OPTION_MAXBIT       = ONIG_OPTION_POSIX_REGION /* limit */

	ONIG_NORMAL   = 0
	ONIG_MISMATCH = -1

	ONIG_MISMATCH_STR                = "mismatch"
	ONIGERR_UNDEFINED_NAME_REFERENCE = -217
)
