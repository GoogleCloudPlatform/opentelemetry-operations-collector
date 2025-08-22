# Tranform Processor with additional custom OTTL functions

This is the same filter processor as [opentelemetry-collector-contrib/processor/filterprocessor/README.md](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/filterprocessor/README.md) with addtional custom OTTL functions.

This processor provides the following additional OTTL functions : 
- [ExtractPatternsRubyRegex](func_extract_patterns_ruby_regex.go)
- [ToValues](func_to_values.go)