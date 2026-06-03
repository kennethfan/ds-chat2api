#!/bin/bash


curl -v -XPOST 'http://localhost:8766/api/command' \
-H 'content-type: application/json' \
-d '{"method":"newSession", "params": {"name": "kenneth", "age": 18}}'
