/**********************************************************************
  chelper.c -  Rubex (https://github.com/moovweb/rubex)
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

#include "oniguruma.h"

extern int NewOnigRegex( char *pattern, int pattern_length, int option,
                                  OnigRegex *regex, OnigEncoding *encoding, OnigErrorInfo **error_info, char **error_buffer);

extern int SearchOnigRegex( void *str, int str_length, int offset, int option,
                                  OnigRegex regex, OnigErrorInfo *error_info, char *error_buffer, int *captures, int *numCaptures);

extern int MatchOnigRegex( void *str, int str_length, int offset, int option,
                  OnigRegex regex);

extern int LookupOnigCaptureByName(char *name, int name_length, OnigRegex regex);

extern int GetCaptureNames(OnigRegex regex, void *buffer, int bufferSize, int* groupNumbers);
