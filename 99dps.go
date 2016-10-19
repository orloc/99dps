package main

import (
	"github.com/hpcloud/tail"
	_ "github.com/mattn/go-gtk/glib"
	_ "github.com/mattn/go-gtk/gtk"
	"log"
)

const EVENT_DISPLAY = "do_print"

func main() {

	/*
		gtk.Init(nil)
		window := gtk.NewWindow(gtk.WINDOW_TOPLEVEL)
		window.SetPosition(gtk.WIN_POS_CENTER)
		window.SetTitle("GTK Go!")
		window.SetIconName("gtk-dialog-info")
		window.Connect("destroy", func(ctx *glib.CallbackContext) {
			println("got destroy!", ctx.Data().(string))
			gtk.MainQuit()
		}, "foo")

		vbox := gtk.NewVBox(false, 1)

		vpaned := gtk.NewVPaned()
		vbox.Add(vpaned)

		frame2 := gtk.NewFrame("Demo")
		framebox2 := gtk.NewVBox(false, 1)
		frame2.Add(framebox2)

		vpaned.Pack2(frame2, false, false)

		//--------------------------------------------------------
		// GtkTextView
		//--------------------------------------------------------
		swin := gtk.NewScrolledWindow(nil, nil)
		swin.SetPolicy(gtk.POLICY_AUTOMATIC, gtk.POLICY_AUTOMATIC)
		swin.SetShadowType(gtk.SHADOW_IN)
		textview := gtk.NewTextView()
		var start, end gtk.TextIter
		buffer := textview.GetBuffer()
		buffer.GetStartIter(&start)
		buffer.Insert(&start, "Hello ")
		buffer.GetEndIter(&end)
		buffer.Insert(&end, "World!")
		tag := buffer.CreateTag("bold", map[string]string{
			"background": "#FF0000", "weight": "700"})
		buffer.GetStartIter(&start)
		buffer.GetEndIter(&end)
		buffer.ApplyTag(tag, &start, &end)
		swin.Add(textview)
		framebox2.Add(swin)

		buffer.Connect("changed", func() {
			println("changed")
		})

		window.SetSizeRequest(600, 600)
		window.ShowAll()
		gtk.Main()
	*/
	activeFile := loadFile()

	inputChan := make(chan string)
	session := CombatSession{}

	go scanInput(inputChan)
	go doParse(activeFile, &session)

	for {
		newInput := <-inputChan
		switch newInput {
		case EVENT_DISPLAY:
			session.Display()
			break
		}
	}

}

func doParse(t *tail.Tail, session *CombatSession) {
	parser := DmgParser{}

	if !session.IsStarted() {
		session.Init()
	}

	for line := range t.Lines {
		if parser.HasDamage(line.Text) {
			dmgSet := parser.ParseDamage(line.Text)
			session.AdjustDamage(dmgSet)
			//session.Display()
		}
	}
}

func checkErr(err interface{}) {
	if err != nil {
		log.Fatal(err)
	}
}
