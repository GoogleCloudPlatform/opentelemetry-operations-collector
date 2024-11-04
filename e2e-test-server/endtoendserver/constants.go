// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package endtoendserver

import (
	"log"
	"os"
)

var (
	subscriptionMode        string
	projectID               string
	requestSubscriptionName string
	port                    string
)

func init() {
	subscriptionMode = os.Getenv("SUBSCRIPTION_MODE")
	if subscriptionMode == "" {
		log.Fatalf("environment variable SUBSCRIPTION_MODE must be set")
	}
	projectID = os.Getenv("PROJECT_ID")
	if projectID == "" {
		log.Fatalf("environment variable PROJECT_ID must be set")
	}
	requestSubscriptionName = os.Getenv("REQUEST_SUBSCRIPTION_NAME")
	if requestSubscriptionName == "" {
		log.Fatalf("environment variable REQUEST_SUBSCRIPTION_NAME must be set")
	}
	port = os.Getenv("PORT")
	if port == "" && subscriptionMode == "push" {
		log.Fatalf("environment variable PORT must be set for push subscription")
	}
}
