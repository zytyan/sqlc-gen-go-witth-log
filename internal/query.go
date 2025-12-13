package golang

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/plugin-sdk-go/metadata"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
	"github.com/sqlc-dev/sqlc-gen-go/internal/opts"
)

type QueryValue struct {
	Emit        bool
	EmitPointer bool
	Name        string
	DBName      string // The name of the field in the database. Only set if Struct==nil.
	Struct      *Struct
	Typ         string
	SQLDriver   opts.SQLDriver

	// Column is kept so late in the generation process around to differentiate
	// between mysql slices and pg arrays
	Column *plugin.Column

	ZapFieldMethod bool
	ZapObject      bool
}

func (v QueryValue) EmitStruct() bool {
	return v.Emit
}

func (v QueryValue) IsStruct() bool {
	return v.Struct != nil
}

func (v QueryValue) IsPointer() bool {
	return v.EmitPointer && v.Struct != nil
}

func (v QueryValue) isEmpty() bool {
	return v.Typ == "" && v.Name == "" && v.Struct == nil
}

type Argument struct {
	Name string
	Type string
}

func (v QueryValue) Pair() string {
	var out []string
	for _, arg := range v.Pairs() {
		out = append(out, arg.Name+" "+arg.Type)
	}
	return strings.Join(out, ",")
}

// Return the argument name and type for query methods. Should only be used in
// the context of method arguments.
func (v QueryValue) Pairs() []Argument {
	if v.isEmpty() {
		return nil
	}
	if !v.EmitStruct() && v.IsStruct() {
		var out []Argument
		for _, f := range v.Struct.Fields {
			out = append(out, Argument{
				Name: escape(toLowerCase(f.Name)),
				Type: f.Type,
			})
		}
		return out
	}
	return []Argument{
		{
			Name: escape(v.Name),
			Type: v.DefineType(),
		},
	}
}

func (v QueryValue) SlicePair() string {
	if v.isEmpty() {
		return ""
	}
	return v.Name + " []" + v.DefineType()
}

func (v QueryValue) Type() string {
	if v.Typ != "" {
		return v.Typ
	}
	if v.Struct != nil {
		return v.Struct.Name
	}
	panic("no type for QueryValue: " + v.Name)
}

func (v *QueryValue) DefineType() string {
	t := v.Type()
	if v.IsPointer() {
		return "*" + t
	}
	return t
}

func (v *QueryValue) ReturnName() string {
	if v.IsPointer() {
		return "&" + escape(v.Name)
	}
	return escape(v.Name)
}

func (v QueryValue) UniqueFields() []Field {
	seen := map[string]struct{}{}
	fields := make([]Field, 0, len(v.Struct.Fields))

	for _, field := range v.Struct.Fields {
		if _, found := seen[field.Name]; found {
			continue
		}
		seen[field.Name] = struct{}{}
		fields = append(fields, field)
	}

	return fields
}

func (v QueryValue) Params() string {
	if v.isEmpty() {
		return ""
	}
	var out []string
	if v.Struct == nil {
		if !v.Column.IsSqlcSlice && strings.HasPrefix(v.Typ, "[]") && v.Typ != "[]byte" && !v.SQLDriver.IsPGX() {
			out = append(out, "pq.Array("+escape(v.Name)+")")
		} else {
			out = append(out, escape(v.Name))
		}
	} else {
		for _, f := range v.Struct.Fields {
			if !f.HasSqlcSlice() && strings.HasPrefix(f.Type, "[]") && f.Type != "[]byte" && !v.SQLDriver.IsPGX() {
				out = append(out, "pq.Array("+escape(v.VariableForField(f))+")")
			} else {
				out = append(out, escape(v.VariableForField(f)))
			}
		}
	}
	if len(out) <= 3 {
		return strings.Join(out, ",")
	}
	out = append(out, "")
	return "\n" + strings.Join(out, ",\n")
}

func (v QueryValue) ColumnNames() []string {
	if v.Struct == nil {
		return []string{v.DBName}
	}
	names := make([]string, len(v.Struct.Fields))
	for i, f := range v.Struct.Fields {
		names[i] = f.DBName
	}
	return names
}

func (v QueryValue) ColumnNamesAsGoSlice() string {
	if v.Struct == nil {
		return fmt.Sprintf("[]string{%q}", v.DBName)
	}
	escapedNames := make([]string, len(v.Struct.Fields))
	for i, f := range v.Struct.Fields {
		if f.Column != nil && f.Column.OriginalName != "" {
			escapedNames[i] = fmt.Sprintf("%q", f.Column.OriginalName)
		} else {
			escapedNames[i] = fmt.Sprintf("%q", f.DBName)
		}
	}
	return "[]string{" + strings.Join(escapedNames, ", ") + "}"
}

// When true, we have to build the arguments to q.db.QueryContext in addition to
// munging the SQL
func (v QueryValue) HasSqlcSlices() bool {
	if v.Struct == nil {
		return v.Column != nil && v.Column.IsSqlcSlice
	}
	for _, v := range v.Struct.Fields {
		if v.Column.IsSqlcSlice {
			return true
		}
	}
	return false
}

func (v QueryValue) Scan() string {
	var out []string
	if v.Struct == nil {
		if strings.HasPrefix(v.Typ, "[]") && v.Typ != "[]byte" && !v.SQLDriver.IsPGX() {
			out = append(out, "pq.Array(&"+v.Name+")")
		} else {
			out = append(out, "&"+v.Name)
		}
	} else {
		for _, f := range v.Struct.Fields {

			// append any embedded fields
			if len(f.EmbedFields) > 0 {
				for _, embed := range f.EmbedFields {
					if strings.HasPrefix(embed.Type, "[]") && embed.Type != "[]byte" && !v.SQLDriver.IsPGX() {
						out = append(out, "pq.Array(&"+v.Name+"."+f.Name+"."+embed.Name+")")
					} else {
						out = append(out, "&"+v.Name+"."+f.Name+"."+embed.Name)
					}
				}
				continue
			}

			if strings.HasPrefix(f.Type, "[]") && f.Type != "[]byte" && !v.SQLDriver.IsPGX() {
				out = append(out, "pq.Array(&"+v.Name+"."+f.Name+")")
			} else {
				out = append(out, "&"+v.Name+"."+f.Name)
			}
		}
	}
	if len(out) <= 3 {
		return strings.Join(out, ",")
	}
	out = append(out, "")
	return "\n" + strings.Join(out, ",\n")
}

// Deprecated: This method does not respect the Emit field set on the
// QueryValue. It's used by the go-sql-driver-mysql/copyfromCopy.tmpl and should
// not be used other places.
func (v QueryValue) CopyFromMySQLFields() []Field {
	// fmt.Printf("%#v\n", v)
	if v.Struct != nil {
		return v.Struct.Fields
	}
	return []Field{
		{
			Name:   v.Name,
			DBName: v.DBName,
			Type:   v.Typ,
		},
	}
}

func (v QueryValue) VariableForField(f Field) string {
	if !v.IsStruct() {
		return v.Name
	}
	if !v.EmitStruct() {
		return toLowerCase(f.Name)
	}
	return v.Name + "." + f.Name
}

// A struct used to generate methods and fields on the Queries struct
type Query struct {
	Cmd          string
	Comments     []string
	MethodName   string
	FieldName    string
	ConstantName string
	SQL          string
	SourceName   string
	Ret          QueryValue
	Arg          QueryValue
	// Used for :copyfrom
	Table *plugin.Identifier
}

type ZapParam struct {
	Key            string
	Value          string
	Func           string
	IsHelper       bool
	ZapFieldMethod bool
	ZapObject      bool
	Nullable       bool
}

func (q Query) ZapParams() []ZapParam {
	return zapParams(q.Arg)
}

func zapParams(val QueryValue) []ZapParam {
	if val.isEmpty() {
		return nil
	}

	if val.Struct == nil {
		fn := zapFieldFunc(val.DefineType())
		nullable := val.Column != nil && !val.Column.NotNull
		return []ZapParam{{
			Key:            zapParamKey(val.DBName, val.Name),
			Value:          escape(val.Name),
			Func:           fn,
			IsHelper:       strings.HasPrefix(fn, "zapNull"),
			ZapFieldMethod: val.ZapFieldMethod,
			ZapObject:      val.ZapObject,
			Nullable:       nullable,
		}}
	}

	params := make([]ZapParam, 0, len(val.Struct.Fields))
	for _, field := range val.UniqueFields() {
		if len(field.EmbedFields) > 0 {
			for _, embed := range field.EmbedFields {
				fn := zapFieldFunc(embed.Type)
				nullable := embed.Column != nil && !embed.Column.NotNull
				params = append(params, ZapParam{
					Key:            zapParamKey(embed.DBName, embed.Name),
					Value:          val.VariableForField(field) + "." + embed.Name,
					Func:           fn,
					IsHelper:       strings.HasPrefix(fn, "zapNull"),
					ZapFieldMethod: embed.ZapFieldMethod,
					ZapObject:      embed.ZapObject,
					Nullable:       nullable,
				})
			}
			continue
		}
		fn := zapFieldFunc(field.Type)
		nullable := field.Column != nil && !field.Column.NotNull
		params = append(params, ZapParam{
			Key:            zapParamKey(field.DBName, field.Name),
			Value:          val.VariableForField(field),
			Func:           fn,
			IsHelper:       strings.HasPrefix(fn, "zapNull"),
			ZapFieldMethod: field.ZapFieldMethod,
			ZapObject:      field.ZapObject,
			Nullable:       nullable,
		})
	}
	return params
}

func zapParamKey(dbName, fallback string) string {
	if dbName != "" {
		return dbName
	}
	return fallback
}

func zapFieldFunc(goType string) string {
	typ := strings.TrimSpace(goType)
	if typ == "" {
		return "zap.Any"
	}
	if strings.HasPrefix(typ, "*") {
		return "zap.Any"
	}
	if strings.HasPrefix(typ, "[]") {
		base := strings.TrimPrefix(typ, "[]")
		if strings.HasPrefix(base, "*") {
			return "zap.Any"
		}
		if base == "byte" || base == "uint8" {
			return "zap.ByteString"
		}
		return "zap.Any"
	}

	switch typ {
	case "sql.NullString":
		return "zapNullString"
	case "sql.NullBool":
		return "zapNullBool"
	case "sql.NullInt32":
		return "zapNullInt32"
	case "sql.NullInt64":
		return "zapNullInt64"
	case "sql.NullFloat64":
		return "zapNullFloat64"
	case "sql.NullTime":
		return "zapNullTime"
	case "string":
		return "zap.String"
	case "bool":
		return "zap.Bool"
	case "int":
		return "zap.Int"
	case "int8":
		return "zap.Int8"
	case "int16":
		return "zap.Int16"
	case "int32":
		return "zap.Int32"
	case "int64":
		return "zap.Int64"
	case "uint":
		return "zap.Uint"
	case "uint8":
		return "zap.Uint8"
	case "uint16":
		return "zap.Uint16"
	case "uint32":
		return "zap.Uint32"
	case "uint64":
		return "zap.Uint64"
	case "uintptr":
		return "zap.Uintptr"
	case "float32":
		return "zap.Float32"
	case "float64":
		return "zap.Float64"
	case "time.Time":
		return "zap.Time"
	case "time.Duration":
		return "zap.Duration"
	case "error":
		return "zap.Error"
	default:
		return "zap.Any"
	}
}

func (q Query) hasRetType() bool {
	scanned := q.Cmd == metadata.CmdOne || q.Cmd == metadata.CmdMany ||
		q.Cmd == metadata.CmdBatchMany || q.Cmd == metadata.CmdBatchOne
	return scanned && !q.Ret.isEmpty()
}

func (q Query) TableIdentifierAsGoSlice() string {
	escapedNames := make([]string, 0, 3)
	for _, p := range []string{q.Table.Catalog, q.Table.Schema, q.Table.Name} {
		if p != "" {
			escapedNames = append(escapedNames, fmt.Sprintf("%q", p))
		}
	}
	return "[]string{" + strings.Join(escapedNames, ", ") + "}"
}

func (q Query) TableIdentifierForMySQL() string {
	escapedNames := make([]string, 0, 3)
	for _, p := range []string{q.Table.Catalog, q.Table.Schema, q.Table.Name} {
		if p != "" {
			escapedNames = append(escapedNames, fmt.Sprintf("`%s`", p))
		}
	}
	return strings.Join(escapedNames, ".")
}
