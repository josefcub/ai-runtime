#!/bin/sh
cd .. && go clean && go build && mv harness UAT/
cd client/ && go clean && go build && mv client ../UAT/
cd ../UAT/
