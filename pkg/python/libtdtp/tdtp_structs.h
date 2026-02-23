#ifndef TDTP_STRUCTS_H
#define TDTP_STRUCTS_H

/* D_Field mirrors packet.Field. */
typedef struct {
    char name[256];
    char type_name[64];
    int  length;
    int  precision;
    int  scale;
    int  is_key;
    int  is_readonly;
} D_Field;

/* D_Schema mirrors packet.Schema. */
typedef struct {
    D_Field* fields;
    int      field_count;
} D_Schema;

/* D_Row mirrors packet.Row after parsing. */
typedef struct {
    char** values;
    int    value_count;
} D_Row;

/* D_Packet is the primary result/argument struct for Direct functions. */
typedef struct {
    D_Row*    rows;
    int       row_count;
    D_Schema  schema;
    char      msg_type[32];
    char      table_name[256];
    char      message_id[64];
    long long timestamp_unix;
    char      compression[16];
    char      error[1024];
} D_Packet;

/* D_FilterSpec describes a single filter condition. */
typedef struct {
    char field[256];
    char op[32];
    char value[1024];
    char value2[1024];
} D_FilterSpec;

/* D_MaskConfig specifies which fields to mask. */
typedef struct {
    char** fields;
    int    field_count;
    char   mask_char[4];
    int    visible_chars;
} D_MaskConfig;

#endif /* TDTP_STRUCTS_H */
