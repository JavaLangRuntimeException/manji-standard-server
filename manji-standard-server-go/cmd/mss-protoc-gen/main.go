// mss-protoc-gen は proto メッセージから DDD の Entity / Repository interface /
// Repository Mock / Postgres (GORM) Repository 実装 / Usecase interface / Handler /
// DI 配線を生成する。
//
// テンプレートは `generator/<kind>/output/<ターゲットパス>/<name>.tpl` に置き、
// //go:embed で埋め込む。各テンプレートの出力先は目的のファイルパスをミラーする。
//
// アノテーション（proto コメント上に記述）:
//
//	@entity       メッセージ全体に付与。Entity / Repository / Mock / Postgres 実装を生成。
//	@pk           フィールドに付与。主キー。SelectByPK / Delete / BulkDelete が生成される。
//	@unique       フィールドに付与。SelectBy<Field> メソッドが追加で生成される。
//	@email        フィールドに付与。email 形式バリデーション。
//	@required     フィールドに付与。非空バリデーション。
//	@timestamp    int64 フィールドに付与。Entity 側で time.Time にマップされる。
//
// service が宣言されていれば、加えて:
//   - pkg/usecase/<service>_usecase.gen.go  Usecase interface + Input 型
//   - pkg/handler/<service>_handler.gen.go  Connect Handler（薄いラッパ）
//   - pkg/di/handlers.gen.go                全 Handler を束ねた DI 配線
//
// を生成する。
package main

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	goImportEntity     = "github.com/example/manji-standard-server-go/internal/domain/entity"
	goImportRepository = "github.com/example/manji-standard-server-go/internal/domain/repository"
	goImportMock       = "github.com/example/manji-standard-server-go/internal/domain/repository/mock"
	goImportInfraRepo  = "github.com/example/manji-standard-server-go/internal/infra/repository"
	goImportDTO        = "github.com/example/manji-standard-server-go/internal/dto"
	goImportUsecase    = "github.com/example/manji-standard-server-go/internal/usecase"
	goImportHandler    = "github.com/example/manji-standard-server-go/internal/handler"
	goImportDI         = "github.com/example/manji-standard-server-go/internal/di"
)

//go:embed generator
var templatesFS embed.FS

// entityKind はエンティティ 1 つにつき 1 ファイルを出力する種別。
type entityKind struct {
	name     string
	tplPath  string
	outPath  func(snake string) string
	importAs string
}

var entityKinds = []entityKind{
	{
		name:    "entity",
		tplPath: "generator/entity/output/internal/domain/entity/entity.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/domain/entity/%s.gen.go", snake)
		},
		importAs: goImportEntity,
	},
	{
		name:    "repository",
		tplPath: "generator/repository/output/internal/domain/repository/repository.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/domain/repository/%s_repository.gen.go", snake)
		},
		importAs: goImportRepository,
	},
	{
		name:    "mock",
		tplPath: "generator/mock/output/internal/domain/repository/mock/mock_repository.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/domain/repository/mock/mock_%s_repository.gen.go", snake)
		},
		importAs: goImportMock,
	},
	{
		name:    "infra_postgres_repository",
		tplPath: "generator/infra_postgres_repository/output/internal/infra/repository/postgres_repository.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/infra/repository/%s_postgres_repository.gen.go", snake)
		},
		importAs: goImportInfraRepo,
	},
	{
		name:    "dto",
		tplPath: "generator/dto/output/internal/dto/dto.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/dto/%s.gen.go", snake)
		},
		importAs: goImportDTO,
	},
}

// serviceKind は service 1 つにつき 1 ファイルを出力する種別。
type serviceKind struct {
	name     string
	tplPath  string
	outPath  func(snake string) string
	importAs string
}

var serviceKinds = []serviceKind{
	{
		name:    "usecase",
		tplPath: "generator/usecase/output/internal/usecase/usecase.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/usecase/%s_usecase_interface.gen.go", snake)
		},
		importAs: goImportUsecase,
	},
	{
		name:    "handler_rest",
		tplPath: "generator/handler_rest/output/internal/handler/handler.gen.go.tpl",
		outPath: func(snake string) string {
			return fmt.Sprintf("internal/handler/%s_handler.gen.go", snake)
		},
		importAs: goImportHandler,
	},
}

// projectKind はプロジェクト全体で 1 ファイルだけ出す種別（DI 配線など）。
type projectKind struct {
	name     string
	tplPath  string
	outPath  string
	importAs string
}

var projectKinds = []projectKind{
	{
		name:     "di",
		tplPath:  "generator/di/output/internal/di/handlers.gen.go.tpl",
		outPath:  "internal/di/handlers.gen.go",
		importAs: goImportDI,
	},
	{
		name:     "entity_registry",
		tplPath:  "generator/entity_registry/output/internal/domain/entity/registry.gen.go.tpl",
		outPath:  "internal/domain/entity/registry.gen.go",
		importAs: goImportEntity,
	},
}

// ==================== data types ====================

type tplField struct {
	GoName      string
	ParamName   string
	SnakeName   string
	ProtoType   string
	GoType      string
	PbFieldGo   string // pb 側での Go フィールド名（timestamp だと末尾 "Unix"）
	JSONTag     string // entity struct の json tag。@timestamp なら proto field 名から末尾 "_unix" を除去、それ以外は proto field 名そのまま
	IsPK        bool
	IsUnique    bool
	IsEmail     bool
	IsRequired  bool
	IsTimestamp bool
	IsPaging    bool
}

type tplData struct {
	Name              string
	SnakeName         string
	LowerFirst        string
	Plural            string
	Receiver          string
	Fields            []tplField
	PKField           tplField
	UniqueFieldsNonPK []tplField
	PagingField       *tplField
	ImportEntity      string
	ImportRepository  string
	ImportMock        string
	ImportInfraRepo   string
}

// usecase / handler 向けのデータ構造。

type tplInputField struct {
	GoName        string // Input struct フィールド名（initialism 正規化済み、例 "ID"）
	JsonName      string // JSON / URL param の元名（例 "id"、proto のフィールド名そのまま）
	ParamName     string // "email"
	GoType        string // usecase package 視点での Go 型（例 "*AdminTourInput" / "*entity.Tour"）
	HandlerGoType string // handler package 視点での Go 型（非 entity message は "usecase." prefix 付き）
	IsPath        bool   // path param 由来
	IsQuery       bool   // GET/DELETE 時に query string 由来として読むフィールド（path 以外で query 化可能な scalar）
	IsBody        bool   // body 由来（POST/PUT/PATCH 用）
	QueryGoType   string // query string から読む際の scalar 型 ("string"/"int32"/"int64"/"bool"/"")
}

type tplHttp struct {
	Method string // "GET" / "POST" / ...
	Path   string // "/api/users/{id}"
}

type tplMethod struct {
	Name              string          // "CreateUser"
	InputTypeName     string          // "CreateUserInput"
	InputFields       []tplInputField // Request を展開した input 型フィールド（全件）
	PathParamFields   []tplInputField // URL の {name} に対応
	BodyFields        []tplInputField // それ以外（POST/PUT/PATCH の body に入る）
	QueryFields       []tplInputField // GET/DELETE で query string から読むフィールド
	HasInputFields    bool
	HasPathParams     bool
	HasBodyFields     bool
	HasQueryFields    bool
	IsBodyMethod      bool   // POST / PUT / PATCH。body decode を行う
	IsQueryMethod     bool   // GET / DELETE。body decode しない（query string から組み立てる）
	EntityGoName      string // 対応する @entity の Go 名（Response の単一フィールドが entity なら設定）
	EntityLowerFirst  string
	ReturnsEntity     bool             // Response が単一 entity フィールド
	ReturnsList       bool             // Response が repeated entity フィールド
	ReturnsEmpty      bool             // それ以外（Empty など）
	ReturnsOutput     bool             // Response を Output struct として展開する
	OutputTypeName    string           // "GetTourOutput"
	OutputMessageName string           // proto Response message 名（参考）
	OutputFields      []tplOutputField // Output struct フィールド
	Http              *tplHttp
}

// tplOutputField は Output struct（および中間 message）の 1 フィールド。
type tplOutputField struct {
	GoName  string
	GoType  string
	JsonTag string // proto field の元名（snake_case）
}

// tplOutputType は Output struct もしくは Output から参照される非 entity message の Go 型定義。
type tplOutputType struct {
	Name   string
	Fields []tplOutputField
}

type tplService struct {
	ServiceName       string // "UserService"
	ServiceSnake      string // "user_service"
	HandlerTypeName   string // "UserHandler"
	UsecaseTypeName   string // "UserUsecase"
	UsecaseParamName  string // "userUsecase"
	Methods           []tplMethod
	UsedEntities      []tplEntityRef  // Handler が使う entity 群（変換関数生成用）
	AnyReturnsEntity  bool            // entity import の要否を判定（戻り値）
	AnyUsesEntity     bool            // entity import の要否を判定（戻り値 or Input/Output フィールド）
	HandlerUsesEntity bool            // handler 側の entity import 要否（戻り値 or Input フィールド。Output は usecase 側に出るため対象外）
	AnyReturnsOutput  bool            // Output 型分岐の要否
	NonEntityTypes    []tplOutputType // Input / Output から参照される非 entity message の Go 構造体定義（重複排除済み、安定ソート）
	NeedsStrconv      bool            // handler で strconv が必要（query string の int パース）
	ImportEntity      string
	ImportRepository  string
	ImportDTO         string
	ImportUsecase     string
}

type tplEntityRef struct {
	Name       string     // "User"
	LowerFirst string     // "user"
	SnakeName  string     // "user"
	Fields     []tplField // entity の全フィールド（pb 変換に使う）
}

type tplDI struct {
	Services      []tplService
	ImportUsecase string
	ImportHandler string
}

// ==================== main ====================

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		tmpls, err := loadTemplates()
		if err != nil {
			return err
		}

		// Pass 1: すべての @entity メッセージを収集（service 解析時に参照）
		entitiesByMsg := map[protoreflect.FullName]*entitySpec{}
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			for _, msg := range f.Messages {
				e, err := parseEntity(msg)
				if err != nil {
					return err
				}
				if e != nil {
					entitiesByMsg[msg.Desc.FullName()] = e
					// pb フィールド名を記録するために msg も一緒に持つ
					e.PbMessage = msg
				}
			}
		}

		// Pass 2: エンティティ kind を実行
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			for _, msg := range f.Messages {
				e, ok := entitiesByMsg[msg.Desc.FullName()]
				if !ok {
					continue
				}
				data := toTplData(e)
				for _, kind := range entityKinds {
					if err := execTpl(gen, tmpls[kind.name], kind.outPath(e.SnakeName), kind.importAs, data); err != nil {
						return fmt.Errorf("%s: %w", kind.name, err)
					}
				}
			}
		}

		// Pass 3: service kind を実行
		var allServices []tplService
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			for _, svc := range f.Services {
				s := buildServiceTpl(svc, f, entitiesByMsg)
				allServices = append(allServices, s)
				for _, kind := range serviceKinds {
					if err := execTpl(gen, tmpls[kind.name], kind.outPath(s.ServiceSnake), kind.importAs, s); err != nil {
						return fmt.Errorf("%s: %w", kind.name, err)
					}
				}
			}
		}

		// Pass 4: project kind を実行(kind ごとにデータ構造が異なる)。
		// entity_registry は entity が 1 つでもあれば必ず出す。di は service が 1 つ以上あるときのみ。
		var entityList []tplEntityRef
		for _, e := range entitiesByMsg {
			entityList = append(entityList, tplEntityRef{Name: e.Name, LowerFirst: lowerFirst(e.Name), SnakeName: e.SnakeName})
		}
		sort.Slice(entityList, func(i, j int) bool { return entityList[i].Name < entityList[j].Name })

		sort.Slice(allServices, func(i, j int) bool { return allServices[i].ServiceName < allServices[j].ServiceName })
		di := tplDI{
			Services:      allServices,
			ImportUsecase: goImportUsecase,
			ImportHandler: goImportHandler,
		}

		for _, kind := range projectKinds {
			var data any
			switch kind.name {
			case "di":
				if len(allServices) == 0 {
					continue
				}
				data = di
			case "entity_registry":
				if len(entityList) == 0 {
					continue
				}
				data = struct{ Entities []tplEntityRef }{Entities: entityList}
			default:
				return fmt.Errorf("unknown project kind: %s", kind.name)
			}
			if err := execTpl(gen, tmpls[kind.name], kind.outPath, kind.importAs, data); err != nil {
				return fmt.Errorf("%s: %w", kind.name, err)
			}
		}

		return nil
	})
}

func execTpl(gen *protogen.Plugin, tmpl *template.Template, outPath, importAs string, data any) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	out := gen.NewGeneratedFile(outPath, protogen.GoImportPath(importAs))
	_, err := out.Write(buf.Bytes())
	return err
}

// loadTemplates は embed 済みの .tpl を全てパースして返す。
func loadTemplates() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"goTestValue": goTestValue,
		"gormTag":     gormTag,
	}
	out := map[string]*template.Template{}
	add := func(name, path string) error {
		raw, err := templatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		t, err := template.New(name).Funcs(funcs).Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		out[name] = t
		return nil
	}
	for _, k := range entityKinds {
		if err := add(k.name, k.tplPath); err != nil {
			return nil, err
		}
	}
	for _, k := range serviceKinds {
		if err := add(k.name, k.tplPath); err != nil {
			return nil, err
		}
	}
	for _, k := range projectKinds {
		if err := add(k.name, k.tplPath); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ==================== entity parse ====================

type fieldSpec struct {
	GoName      string
	ParamName   string
	SnakeName   string
	ProtoType   string
	GoType      string
	PbFieldGo   string // pb 側での Go フィールド名（@timestamp だと "CreatedAtUnix"、それ以外は同じ）
	IsPK        bool
	IsUnique    bool
	IsEmail     bool
	IsRequired  bool
	IsTimestamp bool
	IsPaging    bool
}

type entitySpec struct {
	Name         string
	SnakeName    string
	Fields       []fieldSpec
	PKField      *fieldSpec
	UniqueFields []fieldSpec
	PagingField  *fieldSpec
	PbMessage    *protogen.Message // pb 変換で参照
}

func parseEntity(msg *protogen.Message) (*entitySpec, error) {
	if !hasMarker(string(msg.Comments.Leading), "@entity") {
		return nil, nil
	}
	name := msg.GoIdent.GoName
	snake := toSnake(name)
	e := &entitySpec{Name: name, SnakeName: snake}

	for _, f := range msg.Fields {
		comment := string(f.Comments.Leading)
		goName := normalizeInitialisms(f.GoName)
		paramName := lowerFirst(f.GoName)
		snakeField := toSnake(f.GoName)
		pbFieldGo := f.GoName // 元の pb 側 Go 名

		isPaging := hasMarker(comment, "@paging")
		if isPaging {
			protoType := f.Desc.Kind().String()
			if err := validatePagingProtoType(protoType); err != nil {
				return nil, fmt.Errorf("message %s: field %q: %w", name, f.GoName, err)
			}
			if e.PagingField != nil {
				return nil, fmt.Errorf("message %s: only one @paging field is allowed, found multiple", name)
			}
		}

		spec := fieldSpec{
			GoName:      goName,
			ParamName:   paramName,
			SnakeName:   snakeField,
			ProtoType:   f.Desc.Kind().String(),
			PbFieldGo:   pbFieldGo,
			IsPK:        hasMarker(comment, "@pk"),
			IsUnique:    hasMarker(comment, "@unique"),
			IsEmail:     hasMarker(comment, "@email"),
			IsRequired:  hasMarker(comment, "@required"),
			IsTimestamp: hasMarker(comment, "@timestamp"),
			IsPaging:    isPaging,
		}

		switch {
		case spec.IsTimestamp:
			spec.GoType = "time.Time"
			trimmed := strings.TrimSuffix(spec.GoName, "Unix")
			spec.GoName = trimmed
			spec.ParamName = lowerFirst(trimmed)
			spec.SnakeName = toSnake(trimmed)
		case spec.ProtoType == "string":
			spec.GoType = "string"
		case spec.ProtoType == "int32":
			spec.GoType = "int32"
		case spec.ProtoType == "int64":
			spec.GoType = "int64"
		case spec.ProtoType == "bool":
			spec.GoType = "bool"
		default:
			spec.GoType = "string"
		}

		e.Fields = append(e.Fields, spec)
		if spec.IsPK {
			pk := spec
			e.PKField = &pk
		}
		if spec.IsUnique {
			e.UniqueFields = append(e.UniqueFields, spec)
		}
		if isPaging {
			pf := spec
			e.PagingField = &pf
		}
	}

	if e.PKField == nil {
		return nil, nil
	}

	if e.PagingField != nil {
		if err := validatePagingFieldHasPKOrUnique(e.PagingField.GoName, e.PagingField.IsPK, e.PagingField.IsUnique); err != nil {
			return nil, fmt.Errorf("message %s: %w", name, err)
		}
	}

	return e, nil
}

// validatePagingProtoType は @paging フィールドの proto 型が許容範囲かを検証する。
func validatePagingProtoType(protoType string) error {
	if protoType != "int64" && protoType != "int32" && protoType != "string" {
		return fmt.Errorf("@paging field has unsupported proto type %q (allowed: int64, int32, string)", protoType)
	}
	return nil
}

// validatePagingFieldHasPKOrUnique は @paging フィールドが @pk または @unique を持つかを検証する。
func validatePagingFieldHasPKOrUnique(goName string, isPK, isUnique bool) error {
	if !isPK && !isUnique {
		return fmt.Errorf("@paging field %q must also have @pk or @unique", goName)
	}
	return nil
}

// ==================== service parse ====================

func buildServiceTpl(svc *protogen.Service, f *protogen.File, entities map[protoreflect.FullName]*entitySpec) tplService {
	_ = f // proto package 情報は REST ハンドラ生成では不要
	serviceName := svc.GoName
	serviceSnake := toSnake(serviceName)
	serviceSnake = strings.TrimSuffix(serviceSnake, "_service")

	usecaseType := strings.TrimSuffix(serviceName, "Service") + "Usecase"
	s := tplService{
		ServiceName:      serviceName,
		ServiceSnake:     serviceSnake,
		HandlerTypeName:  strings.TrimSuffix(serviceName, "Service") + "Handler",
		UsecaseTypeName:  usecaseType,
		UsecaseParamName: lowerFirst(usecaseType),
		ImportEntity:     goImportEntity,
		ImportRepository: goImportRepository,
		ImportDTO:        goImportDTO,
		ImportUsecase:    goImportUsecase,
	}

	// Pass A: rpc 由来で予約される型名を収集（<Method>Input / <Method>Output）。
	// これらと衝突する非 entity message 名は別名にリネームして衝突を避ける。
	reservedNames := map[string]struct{}{}
	for _, m := range svc.Methods {
		reservedNames[m.GoName+"Input"] = struct{}{}
		reservedNames[m.GoName+"Output"] = struct{}{}
	}
	// 非 entity message → usecase package 内 Go 型名 のマップ。
	// 衝突がある場合は disambiguateName でユニーク名に解決する。
	nonEntityRename := map[protoreflect.FullName]string{}
	resolve := func(msg *protogen.Message) string {
		if msg == nil {
			return ""
		}
		if name, ok := nonEntityRename[msg.Desc.FullName()]; ok {
			return name
		}
		raw := nonEntityGoName(msg)
		final := raw
		// rpc 予約名と衝突したら trailing "_" でずらす（連続衝突は "__" と続ける）。
		for {
			if _, clashRpc := reservedNames[final]; !clashRpc {
				// 既に同名の別 message が登録されていないかも確認
				dup := false
				for _, used := range nonEntityRename {
					if used == final {
						dup = true
						break
					}
				}
				if !dup {
					break
				}
			}
			final += "_"
		}
		nonEntityRename[msg.Desc.FullName()] = final
		return final
	}

	entityRefByName := map[string]tplEntityRef{}
	nonEntityTypeByName := map[string]tplOutputType{}

	for _, m := range svc.Methods {
		input := m.Input
		output := m.Output

		method := tplMethod{
			Name:          m.GoName,
			InputTypeName: m.GoName + "Input",
			Http:          parseHttpAnnotation(string(m.Comments.Leading)),
		}
		if method.Http != nil {
			switch method.Http.Method {
			case "POST", "PUT", "PATCH":
				method.IsBodyMethod = true
			case "GET", "DELETE":
				method.IsQueryMethod = true
			}
		}

		pathParamSet := map[string]struct{}{}
		if method.Http != nil {
			for _, p := range extractPathParams(method.Http.Path) {
				pathParamSet[p] = struct{}{}
			}
		}

		// Input 型のフィールドは Request メッセージをそのまま展開
		for _, reqField := range input.Fields {
			goName := normalizeInitialisms(reqField.GoName)
			jsonName := string(reqField.Desc.Name()) // proto 側の元フィールド名（snake 不使用、proto3 はそのまま）
			// Output と同じ規則で Go 型を解決する（非 entity message は同パッケージの struct を指す）
			goType := goTypeFromMessageField(reqField, entities, resolve)
			_, isPath := pathParamSet[jsonName]
			field := tplInputField{
				GoName:        goName,
				JsonName:      jsonName,
				ParamName:     lowerFirst(reqField.GoName),
				GoType:        goType,
				HandlerGoType: handlerGoType(goType),
				IsPath:        isPath,
				QueryGoType:   queryScalarGoType(reqField),
			}
			if !isPath {
				if method.IsQueryMethod {
					field.IsQuery = true
				} else {
					field.IsBody = true
				}
			}
			if strings.Contains(goType, "entity.") {
				s.AnyUsesEntity = true
				s.HandlerUsesEntity = true
			}
			// Input が参照する非 entity message を再帰的に収集（usecase ファイル内に struct emit するため）
			collectReferencedNonEntityTypes(reqField, entities, nonEntityTypeByName, resolve)
			method.InputFields = append(method.InputFields, field)
			if isPath {
				method.PathParamFields = append(method.PathParamFields, field)
			} else if method.IsQueryMethod {
				method.QueryFields = append(method.QueryFields, field)
			} else {
				method.BodyFields = append(method.BodyFields, field)
			}
		}
		method.HasInputFields = len(method.InputFields) > 0
		method.HasPathParams = len(method.PathParamFields) > 0
		method.HasBodyFields = len(method.BodyFields) > 0
		method.HasQueryFields = len(method.QueryFields) > 0

		// Response の形状を調べ、単一 entity / repeated entity / Output / Empty を判定
		// 単一 entity フィールドのみの response は従来どおり (*entity.X, error) を返す。
		// それ以外（複数フィールド / プリミティブ / 非 entity message）は Output struct を生成する。
		if len(output.Fields) == 1 {
			resField := output.Fields[0]
			if resField.Message != nil && !resField.Desc.IsMap() {
				if ent, ok := entities[resField.Message.Desc.FullName()]; ok {
					method.EntityGoName = ent.Name
					method.EntityLowerFirst = lowerFirst(ent.Name)
					if resField.Desc.IsList() {
						method.ReturnsList = true
					} else {
						method.ReturnsEntity = true
					}
					// pb 変換で使う entity を登録
					if _, seen := entityRefByName[ent.Name]; !seen {
						ref := tplEntityRef{
							Name:       ent.Name,
							LowerFirst: lowerFirst(ent.Name),
							SnakeName:  ent.SnakeName,
						}
						for _, f := range ent.Fields {
							ref.Fields = append(ref.Fields, toTplField(f))
						}
						entityRefByName[ent.Name] = ref
					}
				}
			}
		}

		if !method.ReturnsEntity && !method.ReturnsList {
			if len(output.Fields) == 0 {
				method.ReturnsEmpty = true
			} else {
				method.ReturnsOutput = true
				method.OutputTypeName = m.GoName + "Output"
				method.OutputMessageName = output.GoIdent.GoName
				for _, of := range output.Fields {
					method.OutputFields = append(method.OutputFields, buildOutputField(of, entities, resolve))
				}
				// Output が参照する非 entity message を再帰的に収集
				// (response message そのものは Output type として展開済みなので skip し、
				// その配下のフィールドが指す message のみ走査する)
				for _, of := range output.Fields {
					collectReferencedNonEntityTypes(of, entities, nonEntityTypeByName, resolve)
				}
				// Output が entity を参照するフィールドは DTO 型（*dto.XDTO）として emit する
				// （outputGoType 参照）。よって entity ではなく dto package の import が必要。
				if outputUsesEntity(output, entities) {
					s.AnyReturnsEntity = true
				}
				s.AnyReturnsOutput = true
			}
		}
		if method.ReturnsEntity || method.ReturnsList {
			s.AnyReturnsEntity = true
			// 戻り値は dto.<Name>DTO に変換してから返すので、handler/usecase いずれも
			// entity package を import する必要はない（dto package のみ参照する）。
		}

		// strconv の要否判定（query string で int32 / int64 をパースする場合）
		if method.IsQueryMethod {
			for _, qf := range method.QueryFields {
				if qf.QueryGoType == "int32" || qf.QueryGoType == "int64" {
					s.NeedsStrconv = true
					break
				}
			}
		}

		s.Methods = append(s.Methods, method)
	}

	for _, ref := range entityRefByName {
		s.UsedEntities = append(s.UsedEntities, ref)
	}
	sort.Slice(s.UsedEntities, func(i, j int) bool {
		return s.UsedEntities[i].Name < s.UsedEntities[j].Name
	})

	for _, ot := range nonEntityTypeByName {
		s.NonEntityTypes = append(s.NonEntityTypes, ot)
	}
	sort.Slice(s.NonEntityTypes, func(i, j int) bool {
		return s.NonEntityTypes[i].Name < s.NonEntityTypes[j].Name
	})

	return s
}

// nonEntityResolver は非 entity message の Go 型名を service スコープでユニークに解決する関数。
type nonEntityResolver func(*protogen.Message) string

// buildOutputField は 1 フィールドを Output struct のフィールド表現に変換する。
func buildOutputField(f *protogen.Field, entities map[protoreflect.FullName]*entitySpec, resolve nonEntityResolver) tplOutputField {
	return tplOutputField{
		GoName:  normalizeInitialisms(f.GoName),
		GoType:  outputGoType(f, entities, resolve),
		JsonTag: string(f.Desc.Name()),
	}
}

// outputGoType は JSON レスポンスに乗る struct（Output struct / 非 entity 中間 struct）の
// フィールド Go 型を解決する。entity を参照するフィールドは **DTO 型**（*dto.XDTO / []*dto.XDTO）に
// マップする。entity は gorm タグのみで json タグを持たないため、そのまま埋めると PascalCase で
// シリアライズされてしまう。DTO 境界（クライアント JSON 表現は DTO が一元管理）に合わせるための変換。
// entity 以外（scalar / 非 entity message / map）は goTypeFromMessageField に委譲する。
func outputGoType(f *protogen.Field, entities map[protoreflect.FullName]*entitySpec, resolve nonEntityResolver) string {
	if !f.Desc.IsMap() && f.Desc.Kind() == protoreflect.MessageKind && f.Message != nil {
		if ent, ok := entities[f.Message.Desc.FullName()]; ok {
			if f.Desc.IsList() {
				return "[]*dto." + ent.Name + "DTO"
			}
			return "*dto." + ent.Name + "DTO"
		}
	}
	return goTypeFromMessageField(f, entities, resolve)
}

// goTypeFromMessageField は Input / Output struct のフィールド型を解決する。
// 非 entity の message は同パッケージ内（usecase package）に struct を生成する前提で
// `*Name` / `[]*Name` を返す。`Name` は resolve(msg) で service スコープのユニーク名に解決する。
// Map は `map[K]V` を返す（V が message の場合は対応する Go 型に再帰解決）。
func goTypeFromMessageField(f *protogen.Field, entities map[protoreflect.FullName]*entitySpec, resolve nonEntityResolver) string {
	if f.Desc.IsMap() {
		keyField := f.Message.Fields[0]
		valField := f.Message.Fields[1]
		return "map[" + scalarGoType(keyField) + "]" + scalarOrMessageGoType(valField, entities, resolve)
	}
	if f.Desc.Kind() == protoreflect.MessageKind && f.Message != nil {
		if _, ok := entities[f.Message.Desc.FullName()]; ok {
			// entity は goTypeFromKind が "*entity.X" / "[]*entity.X" として正しく返す
			return goTypeFromKind(f, entities)
		}
		// 非 entity message → 同パッケージ内に生成される Go 型名を使う（衝突時はリネーム）
		name := resolve(f.Message)
		if f.Desc.IsList() {
			return "[]*" + name
		}
		return "*" + name
	}
	return goTypeFromKind(f, entities)
}

// scalarGoType は map の key などプリミティブ専用の解決。
func scalarGoType(f *protogen.Field) string {
	return goTypeFromKind(f, nil)
}

// queryScalarGoType は GET / DELETE で query string から読む際の scalar 型を返す。
// repeated / message / map のフィールドは query string で扱えないため "" を返す（無視される）。
func queryScalarGoType(f *protogen.Field) string {
	if f.Desc.IsList() || f.Desc.IsMap() {
		return ""
	}
	switch f.Desc.Kind() {
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "int32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "int64"
	default:
		return ""
	}
}

// scalarOrMessageGoType は map の value をプリミティブ or message として解決する。
func scalarOrMessageGoType(f *protogen.Field, entities map[protoreflect.FullName]*entitySpec, resolve nonEntityResolver) string {
	if f.Desc.Kind() == protoreflect.MessageKind && f.Message != nil {
		if ent, ok := entities[f.Message.Desc.FullName()]; ok {
			return "*entity." + ent.Name
		}
		return "*" + resolve(f.Message)
	}
	return goTypeFromKind(f, entities)
}

// nonEntityGoName は非 entity message の Go 型名を返す。
// nested message (e.g. ListRankingToursResponse.Ranked) は親名を join する。
func nonEntityGoName(msg *protogen.Message) string {
	// protogen の GoIdent.GoName は nested の場合 "Parent_Child" 形式になる。
	return msg.GoIdent.GoName
}

// handlerGoType は usecase package 視点の Go 型を handler package 視点に変換する。
// 非 entity message を指す bare な型名（"*Foo" / "[]*Foo" / "map[..]*Foo"）には
// "usecase." prefix を付け、entity.* / primitive / map key 等はそのまま残す。
func handlerGoType(usecaseGoType string) string {
	// "*<ident>" / "[]*<ident>" / "map[..]*<ident>" のいずれの位置にも対応するため、
	// "*" の直後に来る Go identifier を見つけて、ドットを含まずかつ既知 prefix でなければ
	// "usecase." を差し込む。
	var b strings.Builder
	s := usecaseGoType
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if s[i] != '*' {
			continue
		}
		// '*' に続く identifier を読む
		j := i + 1
		for j < len(s) && (isIdentByte(s[j]) || s[j] == '.') {
			j++
		}
		ident := s[i+1 : j]
		if needsUsecasePrefix(ident) {
			b.WriteString("usecase.")
		}
		b.WriteString(ident)
		i = j - 1
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// needsUsecasePrefix は ident（"*" 直後の型名）が usecase package の非 entity 型を
// 指すかを判定する。entity.* 等の package 修飾済み or 空 ident（"**" 連続のような変則）は false。
func needsUsecasePrefix(ident string) bool {
	if ident == "" {
		return false
	}
	if strings.Contains(ident, ".") {
		return false
	}
	// 大文字始まりの bare な型名のみ対象（usecase package で生成される struct は exported）。
	first := ident[0]
	return first >= 'A' && first <= 'Z'
}

// collectReferencedNonEntityTypes は 1 フィールドが参照する message を辿り、
// 非 entity の通常 message なら Go 構造体定義として登録する。map / 子フィールドも再帰。
func collectReferencedNonEntityTypes(f *protogen.Field, entities map[protoreflect.FullName]*entitySpec, out map[string]tplOutputType, resolve nonEntityResolver) {
	if f.Desc.IsMap() {
		// map entry message は struct 化しない。value が message なら追跡。
		if f.Message != nil && len(f.Message.Fields) >= 2 {
			val := f.Message.Fields[1]
			if val.Desc.Kind() == protoreflect.MessageKind && val.Message != nil {
				collectOutputTypes(val.Message, entities, out, resolve)
			}
		}
		return
	}
	if f.Desc.Kind() == protoreflect.MessageKind && f.Message != nil {
		collectOutputTypes(f.Message, entities, out, resolve)
	}
}

// collectOutputTypes は output から参照される非 entity message を再帰的に収集して
// out に Go 構造体定義を登録する。型名は resolve でユニーク化する。
func collectOutputTypes(msg *protogen.Message, entities map[protoreflect.FullName]*entitySpec, out map[string]tplOutputType, resolve nonEntityResolver) {
	// map の場合は entry message そのものを生成しない（map[K]V に展開される）
	if msg == nil {
		return
	}
	if _, ok := entities[msg.Desc.FullName()]; ok {
		return // entity は entity package で別途生成されている
	}
	name := resolve(msg)
	if _, seen := out[name]; seen {
		return
	}
	t := tplOutputType{Name: name}
	for _, f := range msg.Fields {
		t.Fields = append(t.Fields, buildOutputField(f, entities, resolve))
	}
	out[name] = t
	// 子の message を再帰
	for _, f := range msg.Fields {
		if f.Desc.IsMap() {
			// map value が message なら追跡
			if len(f.Message.Fields) >= 2 {
				val := f.Message.Fields[1]
				if val.Desc.Kind() == protoreflect.MessageKind && val.Message != nil {
					collectOutputTypes(val.Message, entities, out, resolve)
				}
			}
			continue
		}
		if f.Desc.Kind() == protoreflect.MessageKind && f.Message != nil {
			collectOutputTypes(f.Message, entities, out, resolve)
		}
	}
}

// outputUsesEntity は output 配下のいずれかのフィールドが entity 型を参照しているかを判定する。
func outputUsesEntity(msg *protogen.Message, entities map[protoreflect.FullName]*entitySpec) bool {
	if msg == nil {
		return false
	}
	for _, f := range msg.Fields {
		if f.Desc.IsMap() {
			if len(f.Message.Fields) >= 2 {
				val := f.Message.Fields[1]
				if val.Desc.Kind() == protoreflect.MessageKind && val.Message != nil {
					if _, ok := entities[val.Message.Desc.FullName()]; ok {
						return true
					}
				}
			}
			continue
		}
		if f.Desc.Kind() == protoreflect.MessageKind && f.Message != nil {
			if _, ok := entities[f.Message.Desc.FullName()]; ok {
				return true
			}
			if outputUsesEntity(f.Message, entities) {
				return true
			}
		}
	}
	return false
}

// @http METHOD /path を leading comment から抽出。
func parseHttpAnnotation(comment string) *tplHttp {
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "@http ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "@http "))
		parts := strings.Fields(rest)
		if len(parts) != 2 {
			continue
		}
		method := strings.ToUpper(parts[0])
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
			return &tplHttp{Method: method, Path: parts[1]}
		}
	}
	return nil
}

func extractPathParams(path string) []string {
	var out []string
	for {
		start := strings.Index(path, "{")
		if start == -1 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end == -1 {
			break
		}
		out = append(out, path[start+1:start+end])
		path = path[start+end+1:]
	}
	return out
}

func goTypeFromKind(f *protogen.Field, entities map[protoreflect.FullName]*entitySpec) string {
	// scalar の repeated は []T、primitive はそのまま返す。
	scalar := func(t string) string {
		if f.Desc.IsList() {
			return "[]" + t
		}
		return t
	}
	switch f.Desc.Kind() {
	case protoreflect.StringKind:
		return scalar("string")
	case protoreflect.BoolKind:
		return scalar("bool")
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return scalar("int32")
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return scalar("int64")
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return scalar("uint32")
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return scalar("uint64")
	case protoreflect.FloatKind:
		return scalar("float32")
	case protoreflect.DoubleKind:
		return scalar("float64")
	case protoreflect.BytesKind:
		// bytes は単独で []byte。repeated bytes は [][]byte。
		if f.Desc.IsList() {
			return "[][]byte"
		}
		return "[]byte"
	case protoreflect.MessageKind:
		// 既知の @entity メッセージなら entity package で参照する。
		// それ以外（ネスト型 / 非 entity の補助 message）は対応する Go 型を持たないため
		// 互換のため string にフォールバック。
		if f.Message != nil {
			if ent, ok := entities[f.Message.Desc.FullName()]; ok {
				if f.Desc.IsList() {
					return "[]*entity." + ent.Name
				}
				return "*entity." + ent.Name
			}
		}
		return "string"
	case protoreflect.EnumKind:
		if f.Enum != nil {
			if f.Desc.IsList() {
				return "[]" + f.Enum.GoIdent.GoName
			}
			return f.Enum.GoIdent.GoName
		}
		return "int32"
	default:
		return "string"
	}
}

// ==================== tpl data for entity kinds ====================

func toTplData(e *entitySpec) tplData {
	lower := lowerFirst(e.Name)
	data := tplData{
		Name:             e.Name,
		SnakeName:        e.SnakeName,
		LowerFirst:       lower,
		Plural:           lower + "s",
		Receiver:         strings.ToLower(string(e.Name[0])),
		ImportEntity:     goImportEntity,
		ImportRepository: goImportRepository,
		ImportMock:       goImportMock,
		ImportInfraRepo:  goImportInfraRepo,
	}
	for _, f := range e.Fields {
		tf := toTplField(f)
		data.Fields = append(data.Fields, tf)
	}
	data.PKField = toTplField(*e.PKField)
	for _, u := range e.UniqueFields {
		if u.IsPK {
			continue
		}
		data.UniqueFieldsNonPK = append(data.UniqueFieldsNonPK, toTplField(u))
	}
	if e.PagingField != nil {
		pf := toTplField(*e.PagingField)
		data.PagingField = &pf
	}
	return data
}

func toTplField(f fieldSpec) tplField {
	return tplField{
		GoName:      f.GoName,
		ParamName:   f.ParamName,
		SnakeName:   f.SnakeName,
		ProtoType:   f.ProtoType,
		GoType:      f.GoType,
		PbFieldGo:   f.PbFieldGo,
		JSONTag:     jsonTagForField(f),
		IsPK:        f.IsPK,
		IsUnique:    f.IsUnique,
		IsEmail:     f.IsEmail,
		IsRequired:  f.IsRequired,
		IsTimestamp: f.IsTimestamp,
		IsPaging:    f.IsPaging,
	}
}

// jsonTagForField は entity struct に付与する JSON tag を決める。
// @timestamp フィールドは proto field 名（例 "created_at_unix"）の末尾 "_unix" を取り除いて
// "created_at" を返す。entity 上の型は time.Time なので _unix を含めると名前と値の不一致になる。
// 通常フィールドは proto field 名そのまま（snake_case）を返す。
func jsonTagForField(f fieldSpec) string {
	// SnakeName は @timestamp の場合 trimmed 名（"created_at"）が入っている。
	// それ以外のフィールドでは proto field 名と同等（toSnake(GoName) で算出済み）。
	return f.SnakeName
}

// ==================== template funcs ====================

func goTestValue(f tplField, suffixExpr string) string {
	if f.IsTimestamp {
		return "time.Unix(0, 0)"
	}
	switch f.GoType {
	case "string":
		prefix := f.SnakeName
		if f.IsEmail {
			return fmt.Sprintf("%q + %s + %q", prefix+"-", suffixExpr, "@example.com")
		}
		return fmt.Sprintf("%q + %s", prefix+"-", suffixExpr)
	case "int32":
		return "int32(1)"
	case "int64":
		return "int64(1)"
	case "bool":
		return "false"
	default:
		return `""`
	}
}

func gormTag(f tplField) string {
	tags := []string{"column:" + f.SnakeName}
	if f.IsPK {
		tags = append(tags, "primaryKey")
	}
	if f.IsUnique && !f.IsPK {
		tags = append(tags, "uniqueIndex")
	}
	if !f.IsPK {
		tags = append(tags, "not null")
	}
	return strings.Join(tags, ";")
}

// ==================== helpers ====================

func hasMarker(comment, marker string) bool {
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "//")
		for _, tok := range strings.Fields(line) {
			if tok == marker {
				return true
			}
		}
	}
	return false
}

func normalizeInitialisms(s string) string {
	repl := map[string]string{
		"Id":   "ID",
		"Url":  "URL",
		"Api":  "API",
		"Http": "HTTP",
		"Json": "JSON",
		"Xml":  "XML",
	}
	for from, to := range repl {
		if s == from {
			return to
		}
		if strings.HasSuffix(s, from) && len(s) > len(from) {
			prev := s[len(s)-len(from)-1]
			if prev >= 'a' && prev <= 'z' {
				s = s[:len(s)-len(from)] + to
			}
		}
	}
	return s
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(string(s[0])) + s[1:]
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
