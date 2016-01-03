package puncher

import (
    "github.com/transhift/common/common"
    "net"
    "fmt"
    "os"
    "sync"
    "github.com/codegangsta/cli"
    "crypto/tls"
    "crypto/rand"
    "time"
)

const (
    CertFileName = "puncher_cert.pem"
    KeyFileName  = "puncher_cert.key"
)

type args struct {
    port   string
    appDir string
}

func (a args) portOrDef(def string) string {
    if len(a.port) == 0 {
        return def
    }

    return a.port
}

func Start(c *cli.Context) {
    args := args{
        port:   c.GlobalString("port"),
        appDir: c.GlobalString("app-dir"),
    }

    storage := &common.Storage{
        CustomDir: args.appDir,
    }

    cert, err := storage.Certificate(CertFileName, KeyFileName)

    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    listener, err := tls.Listen("tcp", net.JoinHostPort("", args.port), &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS12,
    })

    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    fmt.Printf("Listening on port %s\n", args.port)

    for {
        conn, err := listener.Accept()

        if err != nil {
            fmt.Println(os.Stderr, "Error accepting connection:", err)
            continue
        }

        go handleConn(conn)
    }
}

func handleConn(conn net.Conn) {
    defer conn.Close()

    common.LogInfo(conn, "incoming connection")

    in, out := common.MessageChannel(conn)
    msg, ok := <- in

    if ! ok {
        common.HandleError(conn, out, true, "closing connection")
        return
    }

    // Expect ClientType message.
    if msg.Packet != common.ClientType {
        common.HandleError(conn, out, false, "expected client type, got 0x%x", msg)
        return
    }

    switch common.ClientTypeBody(msg.Body[0]) {
    case common.DownloaderClientType:
        handleDownloader(conn, in, out)
    case common.UploaderClientType:
        handleUploader(conn, in, out)
    default:
        common.HandleError(conn, out, true, "expected client type body to be uploader or downloader, got 0x%x", msg)
    }
}

type Downloader struct {
    uid   string
    conn  net.Conn
    ready chan int
}

type DownloaderPool struct {
    sync.RWMutex

    downloaders []Downloader
}

func (p *DownloaderPool) Add(dl *Downloader) {
    dl.ready = make(chan int)

    p.Lock()
    defer p.Unlock()

    p.downloaders = append(p.downloaders, *dl)
}

func (p *DownloaderPool) Find(uid string) (dl *Downloader, exists bool) {
    p.RLock()
    defer p.RUnlock()

    for _, d := range p.downloaders {
        if d.uid == uid {
            return &d, true
        }
    }

    return nil, false
}

func (p *DownloaderPool) Remove(dl *Downloader) {
    p.Lock()
    defer p.Unlock()

    for i, d := range p.downloaders {
        if d.uid == dl.uid {
            p.downloaders = append(p.downloaders[:i], p.downloaders[i + 1:]...)
            break
        }
    }
}

var (
    dlPool = DownloaderPool{}
)

func handleDownloader(conn net.Conn, in chan common.Message, out chan common.Message) {
    var uid string
    var err error

    common.LogInfo(conn, "identified as downloader")

    dlPool.RLock()

    // Generate uid.
    for exists := true; exists; _, exists = dlPool.Find(uid) {
        uid, err = generateUid()

        if err != nil {
            dlPool.RUnlock()
            common.HandleError(conn, out, true, "error generating uid: %s", err)
            return
        }
    }

    dlPool.RUnlock()

    dl := &Downloader{ uid, conn, nil }

    dlPool.Add(dl)
    defer dlPool.Remove(dl)

    // Send uid.
    out <- common.Message{
        Packet: common.UidAssignment,
        Body:   []byte(uid),
    }

    common.LogInfo(conn, "sent uid")

    select {
    // Wait for timeout.
    case <- time.After(time.Hour):
        out <- common.Message{
            Packet: common.Halt,
            Body:   []byte("timeout"),
        }
    // Wait for incoming halt message.
    case msg := <- in:
        if msg.Packet == common.Halt {
            common.LogIncoming(conn, "halt:", string(msg.Body))
        } else {
            common.HandleError(conn, out, false, "expected halt, got 0x%x", msg)
        }
    // Wait for a ready signal from the uploader.
    case <- dl.ready:
        out <- common.Message{ common.UploaderReady, nil }
    }
}

func handleUploader(conn net.Conn, in chan common.Message, out chan common.Message) {
    common.LogInfo(conn, "identified as uploader")
    common.LogInfo(conn, "awaiting uid")

    msg, ok := <- in

    if ! ok {
        common.HandleError(conn, out, true, "closing connection")
        return
    }

    // Expect uid.
    if msg.Packet != common.UidRequest {
        common.HandleError(conn, out, false, "expected uid, got 0x%x", msg)
        return
    }

    uid := string(msg.Body)

    common.LogInfo(conn, "got uid")

    // Validate uid.
    if len(uid) != common.UidLength {
        common.HandleError(conn, out, false, "invalid uid length (not %d), got '%s'", common.UidLength, uid)
        return
    }

    // See if the downloader is waiting.
    dl, waiting := dlPool.Find(uid)

    if waiting {
        // Indicate to the downloader that the uploader is ready.
        dl.ready <- 0

        // Tell the uploader that the downloader is ready.
        out <- common.Message{ common.PeerReady, dl.conn.RemoteAddr() }
    } else {
        // If not, the say that the peer was not found.
        out <- common.Message{ common.PeerNotFound, nil }
        return
    }
}

func generateUid() (string, error) {
    uidBuff := make([]byte, common.UidLength / 2) // 2 hex chars per byte

    if _, err := rand.Read(uidBuff); err != nil {
        return "", err
    }

    return fmt.Sprintf("%x", uidBuff), nil
}
