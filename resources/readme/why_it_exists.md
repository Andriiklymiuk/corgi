## What is my purpose?
As we expand out services and go towards microservices architecture, we need to somehow test many databases, that are started and run locally.

Creation, seeding, recreation of database is pretty cumbersome task, which this cli wants to improve.

It uses docker compose under the hood to run specific db instance in containerized fashion, which helps to start service and stop it, fill with info, etc fast.

## Why GO?

It is written in GOLANG in order to be: fast and simple, without ton of dependencies. Language isn't that different from javascript of typescript, so it can be used by everyone, can be learnt in one day.

This project is also a proof of concept, that Go is simple, fast and easy to be written, so we can use it to create microservices and write automation.

Pros of using go
- easy to use and fast to write production level code
- most things can be done with standard lib itself
- small (if we remove all fancy staff, then the binary will be around 1mb more or less)
- makes you think about error handling during coding itself
- formatting and testing out of the box
- concurrent from the box (parallel simply speaking) (socket will be cool to write in go)

**In short**: install go and you are good to go

[Main docs](../../README.md)