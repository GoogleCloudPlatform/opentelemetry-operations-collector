// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nvmlreceiver

import (
   "context"

   "go.opentelemetry.io/collector/component"
   "go.opentelemetry.io/collector/config"
   "go.opentelemetry.io/collector/consumer"
   "go.opentelemetry.io/collector/receiver/scraperhelper"

   "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver/internal/metadata"
)

const typeStr = "nvml"

func NewFactory() component.ReceiverFactory {
   return component.NewReceiverFactory(
      typeStr,
      createDefaultConfig,
      component.WithMetricsReceiver(createMetricsReceiver, component.StabilityLevelBeta),
   )
}

func createDefaultConfig() config.Receiver {
   return &Config{
      ScraperControllerSettings: scraperhelper.ScraperControllerSettings{
         ReceiverSettings:   config.NewReceiverSettings(config.NewComponentID(typeStr)),
         CollectionInterval: defaultCollectionInterval,
      },
      Metrics: metadata.DefaultMetricsSettings(),
   }
}

func createMetricsReceiver(
   _ context.Context,
   params component.ReceiverCreateSettings,
   rConf config.Receiver,
   consumer consumer.Metrics,
) (component.MetricsReceiver, error) {
   cfg, ok := rConf.(*Config)
   if !ok {
      return nil, nil
   }

   ns, err := newNvmlScraper(cfg, params)
   if err != nil {
      return nil, err
   }

   scraper, err := scraperhelper.NewScraper(
      typeStr,
      ns.scrape,
      scraperhelper.WithStart(ns.start),
      scraperhelper.WithShutdown(ns.stop))
   if err != nil {
      return nil, err
   }

   return scraperhelper.NewScraperControllerReceiver(
      &cfg.ScraperControllerSettings, params, consumer,
      scraperhelper.AddScraper(scraper),
   )
}
