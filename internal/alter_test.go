// Copyright(C) 2022 github.com/hidu  All Rights Reserved.
// Author: hidu <duv123@gmail.com>
// Date: 2022/3/11

package internal

import (
	"testing"
)

func Test_fmtTableCreateSQL(t *testing.T) {
	type args struct {
		sql string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "del auto_incr",
			args: args{
				sql: `CREATE TABLE user (
				id bigint unsigned NOT NULL AUTO_INCREMENT,
				email varchar(1000) NOT NULL DEFAULT '',
				PRIMARY KEY (id)
			) ENGINE=InnoDB AUTO_INCREMENT=3 DEFAULT CHARSET=utf8mb3`,
			},
			want: `CREATE TABLE user (
				id bigint unsigned NOT NULL AUTO_INCREMENT,
				email varchar(1000) NOT NULL DEFAULT '',
				PRIMARY KEY (id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3`,
		},
		{
			name: "del auto_incr 2",
			args: args{
				sql: `CREATE TABLE user (
				id bigint unsigned NOT NULL AUTO_INCREMENT,
				email varchar(1000) NOT NULL DEFAULT '',
				PRIMARY KEY (id)
			) ENGINE=InnoDB AUTO_INCREMENT=4049116 DEFAULT CHARSET=utf8mb4`,
			},
			want: `CREATE TABLE user (
				id bigint unsigned NOT NULL AUTO_INCREMENT,
				email varchar(1000) NOT NULL DEFAULT '',
				PRIMARY KEY (id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmtTableCreateSQL(tt.args.sql); got != tt.want {
				t.Errorf("fmtTableCreateSQL() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_fmtTableCreateSQL_EdgeCases covers regressions around AUTO_INCREMENT=0,
// case-insensitivity, EOL placement, and cases where the clause must NOT match
// (e.g. lowercase column attribute or partial-substring matches).
func Test_fmtTableCreateSQL_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "auto_increment=0 stripped",
			in:   "CREATE TABLE t (id int) ENGINE=InnoDB AUTO_INCREMENT=0 DEFAULT CHARSET=utf8mb4",
			want: "CREATE TABLE t (id int) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		{
			name: "lowercase auto_increment=N stripped via (?i)",
			in:   "CREATE TABLE t (id int) ENGINE=InnoDB auto_increment=42 DEFAULT CHARSET=utf8mb4",
			want: "CREATE TABLE t (id int) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		{
			name: "MixedCase Auto_Increment=N stripped via (?i)",
			in:   "CREATE TABLE t (id int) ENGINE=InnoDB Auto_Increment=99 DEFAULT CHARSET=utf8",
			want: "CREATE TABLE t (id int) ENGINE=InnoDB DEFAULT CHARSET=utf8",
		},
		{
			name: "AUTO_INCREMENT clause at end-of-line preserved newline structure",
			in:   "CREATE TABLE t (\n  id int\n) ENGINE=InnoDB AUTO_INCREMENT=7\n",
			// trailing newline trimmed by fmtTableCreateSQL.TrimRightFunc
			want: "CREATE TABLE t (\n  id int\n) ENGINE=InnoDB",
		},
		{
			name: "AUTO_INCREMENT column attribute (no leading space + '=') NOT stripped",
			in:   "CREATE TABLE t (\n  id bigint unsigned NOT NULL AUTO_INCREMENT,\n  PRIMARY KEY (id)\n) ENGINE=InnoDB",
			want: "CREATE TABLE t (\n  id bigint unsigned NOT NULL AUTO_INCREMENT,\n  PRIMARY KEY (id)\n) ENGINE=InnoDB",
		},
		{
			name: "no AUTO_INCREMENT clause is a no-op",
			in:   "CREATE TABLE t (id int) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
			want: "CREATE TABLE t (id int) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		},
		{
			name: "partition expression after table options does not hide auto increment",
			in: "CREATE TABLE t (\n" +
				"  id int NOT NULL AUTO_INCREMENT,\n" +
				"  PRIMARY KEY (id)\n" +
				") ENGINE=InnoDB AUTO_INCREMENT=42 PARTITION BY HASH(id) PARTITIONS 4",
			want: "CREATE TABLE t (\n" +
				"  id int NOT NULL AUTO_INCREMENT,\n" +
				"  PRIMARY KEY (id)\n" +
				") ENGINE=InnoDB PARTITION BY HASH(id) PARTITIONS 4",
		},
		{
			name: "right parenthesis inside comment does not confuse definition boundary",
			in: "CREATE TABLE t (\n" +
				"  id int,\n" +
				"  note varchar(20) COMMENT 'value ) here'\n" +
				") ENGINE=InnoDB AUTO_INCREMENT=9",
			want: "CREATE TABLE t (\n" +
				"  id int,\n" +
				"  note varchar(20) COMMENT 'value ) here'\n" +
				") ENGINE=InnoDB",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmtTableCreateSQL(tt.in); got != tt.want {
				t.Errorf("fmtTableCreateSQL()\n got:  %q\n want: %q", got, tt.want)
			}
		})
	}
}
