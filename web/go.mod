// web/ holds the web UI and no Go code. This file makes it a module of its
// own, so that ./... in koko's module skips it, and with it the .go files
// some npm packages ship under node_modules.
module koko-web
