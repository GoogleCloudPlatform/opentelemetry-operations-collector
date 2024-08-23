package go_ora

import (
	"database/sql/driver"
	"github.com/sijms/go-ora/v2/configurations"
)

type RefCursor struct {
	defaultStmt
	len        uint8
	MaxRowSize int
	parent     *defaultStmt
}

func (cursor *RefCursor) load() error {
	// initialize ref cursor object
	cursor.text = ""
	cursor._hasLONG = false
	cursor._hasBLOB = false
	cursor._hasReturnClause = false
	//cursor.disableCompression = false
	cursor.arrayBindCount = 1
	cursor.scnForSnapshot = make([]int, 2)
	cursor.stmtType = SELECT
	session := cursor.connection.session
	var err error
	cursor.len, err = session.GetByte()
	if err != nil {
		return err
	}
	cursor.MaxRowSize, err = session.GetInt(4, true, true)
	if err != nil {
		return err
	}
	columnCount, err := session.GetInt(4, true, true)
	if err != nil {
		return err
	}
	if columnCount > 0 {
		cursor.columns = make([]ParameterInfo, columnCount)
		_, err = session.GetByte()
		if err != nil {
			return err
		}
		for x := 0; x < len(cursor.columns); x++ {
			err = cursor.columns[x].load(cursor.connection)
			if err != nil {
				return err
			}
			if cursor.columns[x].DataType == OCIClobLocator || cursor.columns[x].DataType == OCIBlobLocator ||
				cursor.columns[x].DataType == OCIFileLocator {
				cursor._hasBLOB = true
			}
			if cursor.columns[x].isLongType() {
				cursor._hasLONG = true
			}
		}
	}
	_, err = session.GetDlc()
	if err != nil {
		return err
	}
	if session.TTCVersion >= 3 {
		_, err = session.GetInt(4, true, true)
		if err != nil {
			return err
		}
		_, err = session.GetInt(4, true, true)
		if err != nil {
			return err
		}
	}
	if session.TTCVersion >= 4 {
		_, err = session.GetInt(4, true, true)
		if err != nil {
			return err
		}
		_, err = session.GetInt(4, true, true)
		if err != nil {
			return err
		}
	}
	if session.TTCVersion >= 5 {
		_, err = session.GetDlc()
		if err != nil {
			return err
		}
	}
	cursor.cursorID, err = session.GetInt(4, true, true)
	if err != nil {
		return err
	}
	return nil
}
func (cursor *RefCursor) getExeOptions() int {
	if cursor.connection.connOption.Lob == configurations.INLINE {
		return 0x8050
	} else {
		return 0x8040
	}
}
func (cursor *RefCursor) _query() (*DataSet, error) {
	session := cursor.connection.session
	session.ResetBuffer()
	err := cursor.write()
	if err != nil {
		return nil, err
	}
	dataSet := new(DataSet)
	err = cursor.read(dataSet)
	if err != nil {
		return nil, err
	}
	err = cursor.decodePrim(dataSet)
	if err != nil {
		return nil, err
	}
	return dataSet, nil
}
func (cursor *RefCursor) Query() (*DataSet, error) {
	if cursor.connection.State != Opened {
		return nil, driver.ErrBadConn
	}
	tracer := cursor.connection.tracer
	tracer.Printf("Query RefCursor: %d", cursor.cursorID)
	cursor._noOfRowsToFetch = cursor.connection.connOption.PrefetchRows
	cursor._hasMoreRows = true
	if len(cursor.parent.scnForSnapshot) > 0 {
		copy(cursor.scnForSnapshot, cursor.parent.scnForSnapshot)
	}

	dataSet, err := cursor._query()
	if err != nil {
		if isBadConn(err) {
			cursor.connection.setBad()
			tracer.Print("Error: ", err)
			return nil, driver.ErrBadConn
		}
		return nil, err
	}
	return dataSet, nil
}
func (cursor *RefCursor) write() error {
	var define = false
	if cursor.connection.connOption.Lob == configurations.INLINE {
		define = true
	}
	err := cursor.basicWrite(cursor.getExeOptions(), false, define)
	if err != nil {
		return err
	}
	return cursor.connection.session.Write()
}
