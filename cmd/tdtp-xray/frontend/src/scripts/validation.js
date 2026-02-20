/**
 * TDTP X-Ray - Transformation Validation Module
 *
 * Validates ETL SQL transformations for:
 * - Column name conflicts (multi-source)
 * - CAST syntax errors
 * - Naming convention compliance
 */

class TransformationValidator {
    constructor() {
        this.errors = [];
        this.warnings = [];
        this.conflicts = [];
    }

    /**
     * Validates SQL transformation
     * @param {string} sql - SQL transformation code
     * @returns {Object} Validation result
     */
    validate(sql) {
        this.reset();

        // Validate column conflicts
        const conflicts = this.findColumnConflicts(sql);
        if (conflicts.length > 0) {
            this.conflicts = conflicts;
            conflicts.forEach(c => {
                this.errors.push(
                    `Column '${c.columnName}' conflicts between ` +
                    `'${c.firstSource}' and '${c.secondSource}'`
                );
            });
        }

        // Validate CAST syntax
        const castErrors = this.validateCastSyntax(sql);
        if (castErrors.length > 0) {
            castErrors.forEach(e => this.errors.push(e.message));
        }

        // Check naming conventions
        const namingWarnings = this.checkNamingConventions(sql);
        this.warnings.push(...namingWarnings);

        return {
            valid: this.errors.length === 0,
            errors: this.errors,
            warnings: this.warnings,
            conflicts: this.conflicts,
            castErrors: castErrors
        };
    }

    /**
     * Finds column name conflicts in SELECT
     * @param {string} sql - SQL query
     * @returns {Array} Array of conflicts
     */
    findColumnConflicts(sql) {
        // Extract SELECT ... FROM
        const selectMatch = sql.match(/SELECT\s+(.*?)\s+FROM/is);
        if (!selectMatch) return [];

        const selectClause = selectMatch[1];
        const columns = this.parseSelectColumns(selectClause);

        // Find duplicates
        const conflicts = [];
        const seen = new Map();

        columns.forEach(col => {
            const colName = col.alias || col.name;

            if (seen.has(colName)) {
                const prev = seen.get(colName);
                conflicts.push({
                    columnName: colName,
                    firstSource: prev.table,
                    secondSource: col.table,
                    firstRaw: prev.raw,
                    secondRaw: col.raw,
                    suggestion: this.generateConflictSuggestion(prev, col)
                });
            } else {
                seen.set(colName, col);
            }
        });

        return conflicts;
    }

    /**
     * Parses columns from SELECT clause
     * @param {string} selectClause - SELECT clause text
     * @returns {Array} Array of column info objects
     */
    parseSelectColumns(selectClause) {
        const columns = [];

        // Split by comma (simplified)
        const parts = selectClause.split(',');

        parts.forEach(part => {
            part = part.trim();
            if (!part) return;

            const col = { raw: part };

            // Extract alias (AS keyword)
            const asMatch = part.match(/\s+AS\s+\[?(\w+)\]?/i);
            if (asMatch) {
                col.alias = asMatch[1];
            }

            // Extract table.column
            const tableMatch = part.match(/\[?(\w+)\]?\.\[?(\w+)\]?/);
            if (tableMatch) {
                col.table = tableMatch[1];
                col.name = tableMatch[2];
            } else {
                // No table prefix
                const words = part.trim().split(/\s+/);
                col.name = words[words.length - 1].replace(/[\[\]]/g, '');
            }

            columns.push(col);
        });

        return columns;
    }

    /**
     * Generates suggestion for fixing conflict
     * @param {Object} first - First column info
     * @param {Object} second - Second column info
     * @returns {string} Suggestion text
     */
    generateConflictSuggestion(first, second) {
        if (first.table && second.table) {
            return `Use prefixes: [${first.table}].[${first.name}] AS [${first.table}_${first.name}], ` +
                   `[${second.table}].[${second.name}] AS [${second.table}_${second.name}]`;
        }
        return 'Add table prefixes to resolve conflict';
    }

    /**
     * Validates CAST() syntax
     * @param {string} sql - SQL query
     * @returns {Array} Array of syntax errors
     */
    validateCastSyntax(sql) {
        const errors = [];

        // Pattern: CAST(...) AS with comma before alias (common error)
        const badCommaRegex = /CAST\s*\([^)]+\)\s*AS\s*,/gi;
        let match;

        while ((match = badCommaRegex.exec(sql)) !== null) {
            errors.push({
                position: match.index,
                message: 'Syntax error: Comma before CAST alias',
                found: match[0],
                expected: 'CAST(...) AS [column_name]'
            });
        }

        // Pattern: CAST(...) AS without alias
        const noAliasRegex = /CAST\s*\([^)]+\)\s*AS\s+FROM/gi;
        while ((match = noAliasRegex.exec(sql)) !== null) {
            errors.push({
                position: match.index,
                message: 'Missing column alias after CAST() AS',
                found: match[0],
                expected: 'CAST(...) AS [column_name]'
            });
        }

        return errors;
    }

    /**
     * Checks naming conventions
     * @param {string} sql - SQL query
     * @returns {Array} Array of warnings
     */
    checkNamingConventions(sql) {
        const warnings = [];

        // Check for CAST without _C suffix
        const castRegex = /CAST\s*\([^)]+\)\s*AS\s+\[?(\w+)\]?/gi;
        let match;

        while ((match = castRegex.exec(sql)) !== null) {
            const aliasName = match[1];
            if (!aliasName.endsWith('_C')) {
                warnings.push(
                    `CAST field '${aliasName}' should have '_C' suffix. ` +
                    `Consider renaming to '${aliasName}_C'`
                );
            }
        }

        // Check for multi-source without prefixes
        const fromRegex = /FROM\s+(\w+)\s+.*?JOIN\s+(\w+)/is;
        const fromMatch = sql.match(fromRegex);

        if (fromMatch) {
            const selectMatch = sql.match(/SELECT\s+(.*?)\s+FROM/is);
            if (selectMatch) {
                const selectClause = selectMatch[1];

                // Check if columns have table prefixes
                const hasPrefix = /\w+_\w+/.test(selectClause);
                if (!hasPrefix) {
                    warnings.push(
                        'Multi-source query detected but columns may lack source prefixes. ' +
                        'Consider using {SourceName}_{FieldName} format'
                    );
                }
            }
        }

        return warnings;
    }

    /**
     * Generates CAST with proper prefix and suffix
     * @param {string} table - Table name
     * @param {string} column - Column name
     * @param {string} targetType - Target data type
     * @returns {string} CAST SQL statement
     */
    generateCast(table, column, targetType) {
        return `CAST([${table}].[${column}] AS ${targetType}) AS [${table}_${column}_C]`;
    }

    /**
     * Suggests fixes for all found issues
     * @returns {Array} Array of fix suggestions
     */
    suggestFixes() {
        const fixes = [];

        // Fixes for conflicts
        this.conflicts.forEach(conflict => {
            fixes.push({
                type: 'conflict',
                issue: `Column '${conflict.columnName}' conflicts`,
                fix: conflict.suggestion
            });
        });

        return fixes;
    }

    /**
     * Resets validator state
     */
    reset() {
        this.errors = [];
        this.warnings = [];
        this.conflicts = [];
    }
}

// UI Helper Functions

/**
 * Displays validation results in UI
 * @param {Object} result - Validation result
 */
function displayValidationResults(result) {
    const container = document.getElementById('validationResults');
    if (!container) return;

    container.innerHTML = '';

    if (result.valid) {
        container.innerHTML = `
            <div class="validation-success">
                <h3>✅ Validation Passed</h3>
                <ul>
                    <li>No column conflicts detected</li>
                    <li>CAST syntax is correct</li>
                    <li>Safe to proceed</li>
                </ul>
            </div>
        `;
    } else {
        let html = '<div class="validation-errors"><h3>🔴 Validation Failed</h3>';

        // Show errors
        if (result.errors.length > 0) {
            html += '<h4>Errors:</h4><ul>';
            result.errors.forEach(err => {
                html += `<li class="error">❌ ${err}</li>`;
            });
            html += '</ul>';
        }

        // Show conflicts with suggestions
        if (result.conflicts.length > 0) {
            html += '<h4>Suggested Fixes:</h4><ul>';
            result.conflicts.forEach(conflict => {
                html += `<li class="suggestion">💡 ${conflict.suggestion}</li>`;
            });
            html += '</ul>';
        }

        html += '</div>';
        container.innerHTML = html;
    }

    // Show warnings (if any)
    if (result.warnings.length > 0) {
        const warningsHtml = '<div class="validation-warnings"><h4>⚠️ Warnings:</h4><ul>' +
            result.warnings.map(w => `<li>${w}</li>`).join('') +
            '</ul></div>';
        container.innerHTML += warningsHtml;
    }
}

/**
 * Validates SQL in real-time (for textarea)
 * @param {string} sql - SQL code
 */
function validateSQLRealtime(sql) {
    const validator = new TransformationValidator();
    const result = validator.validate(sql);
    displayValidationResults(result);
    return result;
}

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        TransformationValidator,
        displayValidationResults,
        validateSQLRealtime
    };
}
