package gorm

import (
	"fmt"
	"reflect"
	"strings"
)

type JoinTableHandlerInterface interface {
	Table(db *DB) string
	Add(db *DB, source1 interface{}, source2 interface{}) error
	Delete(db *DB, sources ...interface{}) error
	JoinWith(db *DB, source interface{}) *DB
}

type JoinTableForeignKey struct {
	DBName            string
	AssociationDBName string
}

func updateJoinTableHandler(relationship *Relationship) {
	handler := relationship.JoinTableHandler.(*JoinTableHandler)

	destinationScope := &Scope{Value: reflect.New(handler.Destination.ModelType).Interface()}
	for _, primaryField := range destinationScope.GetModelStruct().PrimaryFields {
		db := relationship.ForeignDBName
		handler.Destination.ForeignKeys = append(handler.Destination.ForeignKeys, JoinTableForeignKey{
			DBName:            db,
			AssociationDBName: primaryField.DBName,
		})
	}

	sourceScope := &Scope{Value: reflect.New(handler.Source.ModelType).Interface()}
	for _, primaryField := range sourceScope.GetModelStruct().PrimaryFields {
		db := relationship.AssociationForeignDBName
		handler.Source.ForeignKeys = append(handler.Source.ForeignKeys, JoinTableForeignKey{
			DBName:            db,
			AssociationDBName: primaryField.DBName,
		})
	}
}

type JoinTableSource struct {
	ModelType   reflect.Type
	ForeignKeys []JoinTableForeignKey
}

type JoinTableHandler struct {
	TableName   string          `sql:"-"`
	Source      JoinTableSource `sql:"-"`
	Destination JoinTableSource `sql:"-"`
}

func (s JoinTableHandler) Table(*DB) string {
	return s.TableName
}

func (s JoinTableHandler) GetSearchMap(db *DB, sources ...interface{}) map[string]interface{} {
	values := map[string]interface{}{}

	for _, source := range sources {
		scope := db.NewScope(source)
		modelType := scope.GetModelStruct().ModelType

		if s.Source.ModelType == modelType {
			for _, foreignKey := range s.Source.ForeignKeys {
				values[foreignKey.DBName] = scope.Fields()[foreignKey.AssociationDBName].Field.Interface()
			}
		} else if s.Destination.ModelType == modelType {
			for _, foreignKey := range s.Destination.ForeignKeys {
				values[foreignKey.DBName] = scope.Fields()[foreignKey.AssociationDBName].Field.Interface()
			}
		}
	}
	return values
}

func (s JoinTableHandler) Add(db *DB, source1 interface{}, source2 interface{}) error {
	scope := db.NewScope("")
	searchMap := s.GetSearchMap(db, source1, source2)

	var assignColumns, binVars, conditions []string
	var values []interface{}
	for key, value := range searchMap {
		assignColumns = append(assignColumns, key)
		binVars = append(binVars, `?`)
		conditions = append(conditions, fmt.Sprintf("%v = ?", scope.Quote(key)))
		values = append(values, value)
	}

	for _, value := range searchMap {
		values = append(values, value)
	}

	quotedTable := s.Table(db)
	sql := fmt.Sprintf(
		"INSERT INTO %v (%v) SELECT %v %v WHERE NOT EXISTS (SELECT * FROM %v WHERE %v);",
		quotedTable,
		strings.Join(assignColumns, ","),
		strings.Join(binVars, ","),
		scope.Dialect().SelectFromDummyTable(),
		quotedTable,
		strings.Join(conditions, " AND "),
	)

	return db.Exec(sql, values...).Error
}

func (s JoinTableHandler) Delete(db *DB, sources ...interface{}) error {
	var conditions []string
	var values []interface{}

	for key, value := range s.GetSearchMap(db, sources...) {
		conditions = append(conditions, fmt.Sprintf("%v = ?", key))
		values = append(values, value)
	}

	return db.Table(s.Table(db)).Where(strings.Join(conditions, " AND "), values...).Delete("").Error
}

func (s JoinTableHandler) JoinWith(db *DB, source interface{}) *DB {
	quotedTable := s.Table(db)

	scope := db.NewScope(source)
	modelType := scope.GetModelStruct().ModelType
	var joinConditions []string
	var queryConditions []string
	var values []interface{}
	if s.Source.ModelType == modelType {
		for _, foreignKey := range s.Destination.ForeignKeys {
			destinationTableName := scope.New(reflect.New(s.Destination.ModelType).Interface()).QuotedTableName()
			joinConditions = append(joinConditions, fmt.Sprintf("%v.%v = %v.%v", quotedTable, scope.Quote(foreignKey.DBName), destinationTableName, scope.Quote(foreignKey.AssociationDBName)))
		}

		for _, foreignKey := range s.Source.ForeignKeys {
			queryConditions = append(queryConditions, fmt.Sprintf("%v.%v = ?", quotedTable, scope.Quote(foreignKey.DBName)))
			values = append(values, scope.Fields()[foreignKey.AssociationDBName].Field.Interface())
		}
	} else if s.Destination.ModelType == modelType {
		for _, foreignKey := range s.Source.ForeignKeys {
			sourceTableName := scope.New(reflect.New(s.Source.ModelType).Interface()).QuotedTableName()
			joinConditions = append(joinConditions, fmt.Sprintf("%v.%v = %v.%v", quotedTable, scope.Quote(foreignKey.DBName), sourceTableName, scope.Quote(foreignKey.AssociationDBName)))
		}

		for _, foreignKey := range s.Destination.ForeignKeys {
			queryConditions = append(queryConditions, fmt.Sprintf("%v.%v = ?", quotedTable, scope.Quote(foreignKey.DBName)))
			values = append(values, scope.Fields()[foreignKey.AssociationDBName].Field.Interface())
		}
	}

	return db.Joins(fmt.Sprintf("INNER JOIN %v ON %v", quotedTable, strings.Join(joinConditions, " AND "))).
		Where(strings.Join(queryConditions, " AND "), values...)
}
