#!/bin/bash
set -e
rockhopper --config rockhopper_postgres.yaml up
rockhopper --config rockhopper_postgres.yaml down --to 1
