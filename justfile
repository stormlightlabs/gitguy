# gitguy

# build application & add to PATH
build:
    go build
    go install
    asdf reshim

# run application with api-key flag
run $key:
    gitguy --api-key $key
