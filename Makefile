build:
	go build -o bin/sigbench microsoft.com/sigbench/cmd

agent: build
	bin/sigbench

master: build
	bin/sigbench -l :8000 -mode "service"
