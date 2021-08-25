package anyshare

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/backend/anyshare/api"
	"github.com/rclone/rclone/backend/anyshare/api/efast"
	oauth2Api "github.com/rclone/rclone/backend/anyshare/api/oauth2"
	"github.com/rclone/rclone/backend/anyshare/api/sharedlink"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/dircache"
	"github.com/rclone/rclone/lib/oauthutil"
	"golang.org/x/oauth2"
)

func splitHeader(header string) (string, string, error) {
	index := strings.Index(header, ":")
	if index >= 0 {
		return strings.TrimSpace(header[:index]), strings.TrimSpace(header[index+1:]), nil
	}
	return "", "", errors.New("invalid header")
}

func parseAuthRequest(authRequest []string) (method string, url string, headers map[string]string, err error) {

	if len(authRequest) < 2 {
		err = errors.New("invalid auth request")
		return
	}

	method = authRequest[0]
	url = authRequest[1]

	headers = make(map[string]string)

	for _, header := range authRequest[2:] {
		k, v, err := splitHeader(header)
		if err == nil {
			headers[k] = v
		}
	}

	return
}

type Options struct {
	Host         string `config:"host"`
	Port         string `config:"port"`
	ClientID     string `config:"client_id"`
	ClientSecret string `config:"client_secret"`
}

type Stat struct {
	Name  string
	Id    string
	Size  int64
	MTime time.Time
	Rev   string
}

type Object struct {
	fs     *Fs
	stat   *Stat
	remote string
}

func (o *Object) Fs() fs.Info {
	return o.fs
}

func (o *Object) Hash(ctx context.Context, ty hash.Type) (string, error) {
	return "", hash.ErrUnsupported
}

func (o *Object) ID() string {
	return o.stat.Id
}

func (o *Object) ModTime(ctx context.Context) time.Time {
	return o.stat.MTime
}

func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	if o.stat.Id == "" {
		return nil, errors.New("cant't download - no id")
	}

	var downloadRes efast.FileOsdownloadRes

	_, err := o.fs.srv.R().
		SetContext(ctx).
		SetResult(&downloadRes).
		SetBody(&efast.FileOsdownloadReq{
			Docid: o.stat.Id,
			Rev:   o.stat.Rev,
		}).
		Post("/api/efast/v1/file/osdownload")

	if err != nil {
		fs.Errorf(o, "[anyshare] [osdownload] %s %s", o.remote, err.Error())
		return nil, err
	}

	method, url, headers, err := parseAuthRequest(downloadRes.Authrequest)

	if err != nil {
		fs.Errorf(o, "[anyshare] [parseAuthRequest] invalid auth request")
		return nil, err
	}

	fs.FixRangeOption(options, o.Size())

	fs.OpenOptionAddHeaders(options, headers)

	resp, err := o.fs.fileSrv.R().SetContext(ctx).
		SetHeaders(headers).
		SetDoNotParseResponse(true).
		Execute(method, url)

	if err != nil {
		fs.Errorf(o, "[anyshare] [download stream] %s %s %s", o.remote, method, url)
		return nil, err
	}

	return resp.RawBody(), nil
}

func (o *Object) ParentID() string {
	if len(o.stat.Id) <= 38 {
		return ""
	}

	return o.stat.Id[0 : len(o.stat.Id)-33]
}

func (o *Object) readMetaData(ctx context.Context) (err error) {
	if o.stat != nil {
		return nil
	}

	stat, err := o.fs.readMetaDataForPath(ctx, o.remote)

	if err != nil {
		if apiErr, ok := err.(*api.Error); ok {
			if apiErr.Code == 404 {
				return fs.ErrorObjectNotFound
			}
		}
		return err
	}

	o.stat = stat

	return nil
}

func (o *Object) Remote() string {
	return o.remote
}

func (o *Object) Remove(ctx context.Context) error {
	return o.fs.deleteObject(ctx, o.stat.Id)
}

func (o *Object) SetModTime(ctx context.Context, t time.Time) (err error) {
	return fs.ErrorCantSetModTime
}

func (o *Object) Size() int64 {
	return o.stat.Size
}

func (o *Object) Storable() bool {
	return true
}

func (o *Object) String() string {
	return o.stat.Name
}

func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {

	size := src.Size()
	modTime := src.ModTime(ctx)
	remote := o.Remote()

	leaf, directoryID, err := o.fs.dirCache.FindPath(ctx, remote, true)

	if err != nil {
		return err
	}

	return o.upload(ctx, in, leaf, directoryID, size, modTime, options...)
}

func (o *Object) upload(ctx context.Context, in io.Reader, leaf, directoryID string, size int64, modTime time.Time, options ...fs.OpenOption) (err error) {

	var uploadInfo efast.FileOsbeginuploadRes

	_, err = o.fs.srv.R().
		SetContext(ctx).
		SetResult(&uploadInfo).
		SetBody(&efast.FileOsbeginuploadReq{
			Docid:       directoryID,
			Length:      size,
			Name:        leaf,
			ClientMtime: modTime.UnixNano() / 1e3,
			Ondup:       3,
			Reqmethod:   "PUT",
		}).
		Post("/api/efast/v1/file/osbeginupload")

	if err != nil {
		fs.Errorf(o, "[anyshare][osbeginupload] %s %s", leaf, err.Error())
		return err
	}

	method, url, headers, err := parseAuthRequest(uploadInfo.Authrequest)

	if err != nil {
		fs.Errorf(o, "[anyshare] [parseAuthRequest] invalid auth request")
		return err
	}

	_, err = o.fs.fileSrv.R().
		SetContext(ctx).
		SetHeaders(headers).
		SetBody(in).
		Execute(method, url)

	if err != nil {
		fs.Errorf(o, "[anyshare][upload stream] %s %s %s", leaf, method, url)
		return err
	}

	var endUploadInfo efast.FileOsenduploadRes

	_, err = o.fs.srv.R().
		SetContext(ctx).
		SetResult(&endUploadInfo).
		SetBody(&efast.FileOsenduploadReq{
			Docid: uploadInfo.Docid,
			Rev:   uploadInfo.Rev,
		}).
		Post("/api/efast/v1/file/osendupload")

	if err != nil {
		fs.Errorf(o, "[anyshare][osendupload] %s %s", leaf, err.Error())
		return err
	}

	o.stat = &Stat{
		Name:  uploadInfo.Name,
		Id:    uploadInfo.Docid,
		Size:  size,
		MTime: modTime,
		Rev:   uploadInfo.Rev,
	}

	return nil
}

type Fs struct {
	name     string             // name of this remote
	root     string             // the path we are working on
	opt      Options            // parsed options
	features *fs.Features       // optional features
	dirCache *dircache.DirCache // Map of directory path to directory id
	pacer    *fs.Pacer          // pacer for API calls
	srv      *resty.Client      // the connection to the anyshare server
	fileSrv  *resty.Client
}

func (f *Fs) About(ctx context.Context) (usage *fs.Usage, err error) {

	var result efast.Quota

	_, err = f.srv.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/api/efast/v1/quota/user")

	if err != nil {
		return nil, errors.Wrap(err, "failed to read user info")
	}

	usage = &fs.Usage{
		Used:  fs.NewUsageValue(result.Used),                    // bytes in use
		Total: fs.NewUsageValue(result.Allocated),               // bytes total
		Free:  fs.NewUsageValue(result.Allocated - result.Used), // bytes free
	}

	return usage, nil
}

func (f *Fs) CreateDir(ctx context.Context, pathID, leaf string) (newID string, err error) {

	var result efast.DirCreateRes

	_, err = f.srv.R().SetContext(ctx).
		SetResult(&result).
		SetBody(&efast.DirCreateReq{
			Docid: pathID,
			Name:  leaf,
			Ondup: 3,
		}).
		Post("/api/efast/v1/dir/create")

	if err != nil {
		return "", err
	}

	return result.Docid, nil
}

func (f *Fs) createObject(ctx context.Context, remote string, modTime time.Time, size int64) (o *Object, leaf string, directoryID string, err error) {
	// Create the directory for the object if it doesn't exist
	leaf, directoryID, err = f.dirCache.FindPath(ctx, remote, true)
	if err != nil {
		return
	}
	// Temporary Object under construction
	o = &Object{
		fs:     f,
		remote: remote,
		stat: &Stat{
			Size:  size,
			MTime: modTime,
		},
	}
	return o, leaf, directoryID, nil
}

func (f *Fs) deleteObject(ctx context.Context, id string) error {
	_, err := f.srv.R().SetContext(ctx).SetBody(&efast.FileDeleteReq{
		Docid: id,
	}).Post("/api/efast/v1/file/delete")

	return err
}

func (f *Fs) FindLeaf(ctx context.Context, pathID, leaf string) (pathIDOut string, found bool, err error) {
	found, err = f.listAll(ctx, pathID, true, false, func(stat *Stat) bool {
		if strings.EqualFold(stat.Name, leaf) {
			pathIDOut = stat.Id
			return true
		}
		return false
	})

	return pathIDOut, found, err
}

func (f *Fs) Features() *fs.Features {
	return f.features
}

func (f *Fs) Hashes() hash.Set {
	return hash.Set(hash.None)
}
func (f *Fs) List(ctx context.Context, dir string) (entries fs.DirEntries, err error) {
	directoryID, err := f.dirCache.FindDir(ctx, dir, false)

	fs.Infof(f, "list %s", dir)

	if err == nil {
		_, err = f.listAll(ctx, directoryID, false, false, func(stat *Stat) bool {

			remote := path.Join(dir, stat.Name)

			if stat.Size == -1 {
				f.dirCache.Put(remote, stat.Id)
				d := fs.NewDir(remote, stat.MTime).SetID(stat.Id)
				entries = append(entries, d)
			} else {
				o := &Object{
					fs:     f,
					remote: remote,
					stat:   stat,
				}
				entries = append(entries, o)
			}

			return false
		})
	}

	if err != nil {
		return nil, err
	}

	return entries, nil
}

type listAllFn func(*Stat) bool

func (f *Fs) listAll(ctx context.Context, dirId string, dirOnly bool, fileOnly bool, fn listAllFn) (found bool, err error) {

	if dirId == "" {
		if !fileOnly {
			var result []efast.EntryItem
			_, err = f.srv.R().
				SetContext(ctx).
				SetResult(&result).
				Get("/api/efast/v1/entry-doc-lib")

			if err != nil {
				return false, err
			}

			for _, item := range result {
				stat := &Stat{
					Name:  item.Name,
					Size:  -1,
					Id:    item.Id,
					MTime: item.ModifiedAt,
					Rev:   item.Rev,
				}

				if fn(stat) {
					found = true
					break
				}
			}
		}
	} else {
		var result efast.DirListRes

		_, err = f.srv.R().
			SetContext(ctx).
			SetResult(&result).
			SetBody(&efast.DirListReq{
				Docid: dirId,
				By:    "name",
				Sort:  "asc",
			}).
			Post("/api/efast/v1/dir/list")

		if err != nil {
			return false, err
		}

		if !fileOnly {
			for _, item := range result.Dirs {
				stat := &Stat{
					Name:  item.Name,
					Size:  item.Size,
					Id:    item.Docid,
					MTime: time.Unix(item.Modified/1e6, item.Modified%1e6*1e3),
					Rev:   item.Rev,
				}

				if fn(stat) {
					found = true
					break
				}
			}
		}

		if !dirOnly {
			for _, item := range result.Files {
				stat := &Stat{
					Name:  item.Name,
					Size:  item.Size,
					Id:    item.Docid,
					MTime: time.Unix(item.ClientMtime/1e6, item.ClientMtime%1e6*1e3),
					Rev:   item.Rev,
				}

				if fn(stat) {
					found = true
					break
				}
			}
		}
	}

	return found, nil
}

// Mkdir makes the directory (container, bucket)
//
// Shouldn't return an error if it already exists
func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	_, err := f.dirCache.FindDir(ctx, dir, true)
	return err
}

func (f *Fs) Move(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	srcObj, ok := src.(*Object)
	if !ok {
		fs.Debugf(src, "Can't move - not same remote type")
		return nil, fs.ErrorCantMove
	}

	// Create temporary object
	dstObj, leaf, directoryID, err := f.createObject(ctx, remote, srcObj.ModTime(ctx), srcObj.Size())

	if err != nil {
		return nil, err
	}

	if srcObj.ParentID() == directoryID {
		var result efast.FileRenameRes

		_, err = f.srv.R().
			SetContext(ctx).
			SetResult(&result).
			SetBody(&efast.FileRenameReq{
				Docid: srcObj.ID(),
				Name:  leaf,
				Ondup: 1,
			}).
			Post("/api/efast/v1/file/rename")

		if err != nil {
			return nil, err
		}

		dstObj.stat.Name = result.Name
		dstObj.stat.Id = srcObj.ID()
	} else {

		var result efast.FileMoveRes

		_, err = f.srv.R().
			SetContext(ctx).
			SetResult(&result).
			SetBody(&efast.FileMoveReq{
				Docid:      srcObj.ID(),
				Destparent: directoryID,
				Ondup:      3,
			}).
			Post("/api/efast/v1/file/move")

		if err != nil {
			return nil, err
		}

		dstObj.stat.Name = result.Name
		dstObj.stat.Id = result.Docid
	}

	dstObj.stat.Rev = srcObj.stat.Rev
	return dstObj, nil
}

func (f *Fs) Name() string {
	return f.name
}

func (f *Fs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	return f.newObjectWithInfo(ctx, remote, nil)
}

func (f *Fs) newObjectWithInfo(ctx context.Context, remote string, stat *Stat) (fs.Object, error) {
	o := &Object{
		fs:     f,
		remote: remote,
	}
	var err error
	if stat != nil {
		// Set info
		o.stat = stat
	} else {
		err = o.readMetaData(ctx) // reads info and meta, returning an error
		if err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (f *Fs) Precision() time.Duration {
	return time.Microsecond
}

func (f *Fs) PublicLink(ctx context.Context, remote string, expire fs.Duration, unlink bool) (string, error) {

	id, err := f.dirCache.FindDir(ctx, remote, false)

	itemType := "folder"

	if err != nil {
		fs.Debugf(f, "attempting to share single file '%s'", remote)
		o, err := f.NewObject(ctx, remote)
		if err != nil {
			return "", err
		}

		id = o.(*Object).ID()
		itemType = "file"
	}

	result := make([]*sharedlink.AddItemInfoDocumentRealname, 1)

	_, err = f.srv.R().
		SetContext(ctx).
		SetResult(&result).
		SetQueryParam("type", "realname").
		Get(fmt.Sprintf("/api/shared-link/v1/document/%s/%s", itemType, url.PathEscape(id)))

	if err != nil {
		return "", err
	}

	if len(result) < 1 || result[0] == nil {
		return "", errors.New("create shared link failed")
	}

	return fmt.Sprintf("https://%s:%s/link/%s", f.opt.Host, f.opt.Port, result[0].Id), nil
}

func (f *Fs) Purge(ctx context.Context, dir string) error {
	return f.purgeCheck(ctx, dir, false)
}

func (f *Fs) purgeCheck(ctx context.Context, dir string, check bool) error {
	root := path.Join(f.root, dir)
	if root == "" {
		return errors.New("can't purge root directory")
	}

	rootID, err := f.dirCache.FindDir(ctx, dir, false)

	if err != nil {
		return err
	}

	if check {
		found, err := f.listAll(ctx, rootID, false, false, func(stat *Stat) bool {
			return true
		})
		if err != nil {
			return err
		}
		if found {
			return errors.Errorf("directory not empty")
		}
	}

	_, err = f.srv.R().
		SetContext(ctx).
		SetBody(&efast.DirDeleteReq{
			Docid: rootID,
		}).
		Post("/api/efast/v1/dir/delete")

	if err != nil {
		return errors.Wrap(err, "rmdir failed")
	}

	f.dirCache.FlushDir(dir)

	return nil
}

func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	// If directory doesn't exist, file doesn't exist so can upload
	remote := src.Remote()
	_, _, err := f.dirCache.FindPath(ctx, remote, false)
	if err != nil {
		if err == fs.ErrorDirNotFound {
			return f.PutUnchecked(ctx, in, src, options...)
		}
		return nil, err
	}

	// TODO 预检查文件是否存在

	return f.PutUnchecked(ctx, in, src, options...)
}

func (f *Fs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	return f.Put(ctx, in, src, options...)
}

func (f *Fs) PutUnchecked(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	remote := src.Remote()
	size := src.Size()
	modTime := src.ModTime(ctx)

	o, _, _, err := f.createObject(ctx, remote, modTime, size)
	if err != nil {
		return nil, err
	}
	return o, o.Update(ctx, in, src, options...)
}

func (f *Fs) readMetaDataForPath(ctx context.Context, path string) (stat *Stat, err error) {
	// defer fs.Trace(f, "path=%q", path)("info=%+v, err=%v", &info, &err)
	leaf, directoryID, err := f.dirCache.FindPath(ctx, path, false)
	if err != nil {
		if err == fs.ErrorDirNotFound {
			return nil, fs.ErrorObjectNotFound
		}
		return nil, err
	}

	found, err := f.listAll(ctx, directoryID, false, true, func(s *Stat) bool {
		if strings.EqualFold(s.Name, leaf) {
			stat = s
			return true
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fs.ErrorObjectNotFound
	}
	return stat, nil
}

// Rmdir removes the directory (container, bucket) if empty
//
// Return an error if it doesn't exist or isn't empty
func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	return f.purgeCheck(ctx, dir, true)
}

func (f *Fs) Root() string {
	return f.root
}

func (f *Fs) String() string {
	return fmt.Sprintf("%s", f.root)
}

func (f *Fs) UserInfo(ctx context.Context) (map[string]string, error) {
	var result efast.UserGetRes

	userInfo := make(map[string]string)

	_, err := f.srv.R().
		SetContext(ctx).
		SetResult(&result).
		Post("/api/eacp/v1/user/get")

	if err != nil {
		return userInfo, err
	}

	userInfo["Account"] = result.Account
	userInfo["Name"] = result.Name
	userInfo["ID"] = result.Userid

	if result.Mail != "" {
		userInfo["Mail"] = result.Mail
	}

	if result.Telnumber != "" {
		userInfo["Tel"] = result.Telnumber
	}

	if len(result.Directdepinfos) > 0 {
		departments := make([]string, 0, len(result.Directdepinfos))

		for _, depInfo := range result.Directdepinfos {
			departments = append(departments, depInfo.Name)
		}

		userInfo["Departments"] = strings.Join(departments, ", ")
	}

	return userInfo, nil
}

func NewFs(ctx context.Context, name string, root string, config configmap.Mapper) (fs.Fs, error) {

	opt := new(Options)
	err := configstruct.Set(config, opt)

	if err != nil {
		return nil, err
	}

	rootURL := fmt.Sprintf("https://%s:%s", opt.Host, opt.Port)

	client, _, err := oauthutil.NewClient(ctx, name, config, &oauth2.Config{
		Scopes: nil,
		Endpoint: oauth2.Endpoint{
			AuthURL:  rootURL + "/oauth2/auth",
			TokenURL: rootURL + "/oauth2/token",
		},
		ClientID:     opt.ClientID,
		ClientSecret: opt.ClientSecret,
		RedirectURL:  oauthutil.RedirectURL,
	})

	rootID := ""

	f := &Fs{
		name:    name,
		root:    root,
		opt:     *opt,
		srv:     resty.NewWithClient(client).SetHostURL(rootURL).SetRetryCount(3).SetError(api.Error{}),
		fileSrv: resty.New().SetRetryCount(3),
	}

	f.features = (&fs.Features{
		CaseInsensitive:         true,
		CanHaveEmptyDirectories: true,
	}).Fill(ctx, f)

	f.dirCache = dircache.New(root, rootID, f)

	err = f.dirCache.FindRoot(ctx, false)

	if err != nil {
		// Assume it is a file
		newRoot, remote := dircache.SplitPath(f.root)
		tempF := *f
		tempF.dirCache = dircache.New(newRoot, rootID, &tempF)
		tempF.root = newRoot
		err = tempF.dirCache.FindRoot(ctx, false)
		if err != nil {
			return f, nil
		}
		_, err := tempF.NewObject(ctx, remote)
		if err != nil {
			return f, nil
		}
		f.dirCache = tempF.dirCache
		f.root = tempF.root
		return f, fs.ErrorIsFile
	}

	return f, nil
}

func init() {
	fs.Register(&fs.RegInfo{
		Name:        "anyshare",
		Description: "Backend for AISHU AnyShare",
		NewFs:       NewFs,
		Options: []fs.Option{
			{
				Name:    "host",
				Help:    "Host",
				Default: "anyshare.aishu.cn",
			},
			{
				Name:    "port",
				Help:    "Port",
				Default: 443,
			}, {
				Name:     config.ConfigClientID,
				Help:     "OAuth Client Id\nLeave blank normally.",
				Advanced: true,
			}, {
				Name:     config.ConfigClientSecret,
				Help:     "OAuth Client Secret\nLeave blank normally.",
				Advanced: true,
			},
		},
		Config: func(ctx context.Context, name string, m configmap.Mapper, configIn fs.ConfigIn) (*fs.ConfigOut, error) {

			opt := new(Options)
			err := configstruct.Set(m, opt)

			if err != nil {
				return nil, errors.Wrap(err, "couldn't parse config into struct")
			}

			host, hostOk := m.Get("host")
			port, portOk := m.Get("port")
			clientID, clientIDOk := m.Get("client_id")
			clientSecret, clientSecretOk := m.Get("client_secret")

			if hostOk && portOk && host != "" && port != "" {

				m.Set("host", host)
				m.Set("port", port)

				rootURL := fmt.Sprintf("https://%s:%s", host, port)

				if !clientIDOk || !clientSecretOk || clientID == "" || clientSecret == "" {

					client := resty.New().SetHostURL(rootURL).SetRetryCount(3)

					var oauth2Client oauth2Api.CreateClientSuccess

					_, err := client.R().
						SetContext(ctx).
						SetBody(&oauth2Api.CreateClientRequest{
							RedirectUris:           []string{oauthutil.RedirectURL},
							GrantTypes:             []string{"authorization_code", "refresh_token", "implicit"},
							ResponseTypes:          []string{"token id_token", "code", "token"},
							ClientName:             "Mac客户端",
							Scope:                  "offline openid all",
							PostLogoutRedirectUris: []string{oauthutil.RedirectURL + "fake-post-logout-redirect-url"},
							Metadata: &oauth2Api.ModelMap{
								Device: &oauth2Api.Device{
									Name:        "RichClient",
									ClientType:  "mac_os",
									Description: "RichClient for mac_os",
								},
							},
						}).
						SetResult(&oauth2Client).
						SetHeader("Content-Type", "application/json").
						Post("/oauth2/clients")

					if err != nil {
						return nil, errors.Wrap(err, "register oauth2 client failed")
					}

					clientID = oauth2Client.ClientId
					clientSecret = oauth2Client.ClientSecret

					m.Set("client_id", clientID)
					m.Set("client_secret", clientSecret)
				}

				return oauthutil.ConfigOut("", &oauthutil.Options{
					OAuth2Config: &oauth2.Config{
						Scopes: nil,
						Endpoint: oauth2.Endpoint{
							AuthURL:  rootURL + "/oauth2/auth",
							TokenURL: rootURL + "/oauth2/token",
						},
						ClientID:     clientID,
						ClientSecret: clientSecret,
						RedirectURL:  oauthutil.RedirectURL,
					},
				})
			}

			return nil, errors.New("invalid host or port")
		},
	})
}

var (
	_ fs.Abouter    = (*Fs)(nil)
	_ fs.Fs         = (*Fs)(nil)
	_ fs.Mover      = (*Fs)(nil)
	_ fs.Purger     = (*Fs)(nil)
	_ fs.UserInfoer = (*Fs)(nil)
	_ fs.IDer       = (*Object)(nil)
	_ fs.Object     = (*Object)(nil)
	_ fs.ParentIDer = (*Object)(nil)
)
