#!/usr/bin/env bash
exec corgi logs --all --idle 0 --grep '(?i)\b(error|panic|fatal|exception|traceback|5[0-9]{2} )\b' --silent
