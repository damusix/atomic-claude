-- Realistic T-SQL fixture for #70 phase-2 lineage validation.
-- Conventional CREATE PROCEDURE / CREATE VIEW bodies (not dynamic SQL) so the
-- extractor's routine-body scan actually reaches the temp tables, OUTPUT INTO
-- targets, PIVOT sources, and qualified column references.

CREATE TABLE dbo.Account (
    account_id   INT PRIMARY KEY,
    person_id    INT,
    status       VARCHAR(20),
    balance      DECIMAL(18,2)
);
GO

CREATE TABLE dbo.Person (
    person_id    INT PRIMARY KEY,
    full_name    VARCHAR(120),
    region       VARCHAR(40)
);
GO

CREATE TABLE dbo.Orders (
    order_id     INT PRIMARY KEY,
    account_id   INT,
    order_year   INT,
    amount       DECIMAL(18,2)
);
GO

CREATE TABLE dbo.AuditLog (
    audit_id     INT IDENTITY,
    action       VARCHAR(20),
    account_id   INT
);
GO

-- Local temp table: staged from real tables, then read back (intra-proc lineage).
CREATE PROCEDURE dbo.usp_StageActiveAccounts
AS
BEGIN
    SET NOCOUNT ON;

    CREATE TABLE #ActiveStage (account_id INT, balance DECIMAL(18,2));

    INSERT INTO #ActiveStage (account_id, balance)
    SELECT a.account_id, a.balance
    FROM dbo.Account a
    WHERE a.status = 'active';

    SELECT s.account_id, s.balance
    FROM #ActiveStage s
    ORDER BY s.balance DESC;
END;
GO

-- A SECOND procedure declaring the same #ActiveStage name: must resolve to a
-- DISTINCT node (same-file two-proc isolation).
CREATE PROCEDURE dbo.usp_StageDormantAccounts
AS
BEGIN
    SELECT account_id, balance INTO #ActiveStage
    FROM dbo.Account
    WHERE status = 'dormant';

    SELECT account_id FROM #ActiveStage;
END;
GO

-- Table variable: declared, inserted into, selected from. A scalar @threshold
-- must NOT become a relation.
CREATE PROCEDURE dbo.usp_BatchHighValue
AS
BEGIN
    DECLARE @threshold DECIMAL(18,2) = 1000.00;
    DECLARE @batch TABLE (order_id INT, amount DECIMAL(18,2));

    INSERT INTO @batch (order_id, amount)
    SELECT o.order_id, o.amount
    FROM dbo.Orders o
    WHERE o.amount >= @threshold;

    SELECT b.order_id FROM @batch b;
END;
GO

-- OUTPUT ... INTO a real audit table AND into a table variable.
CREATE PROCEDURE dbo.usp_CloseAccounts
AS
BEGIN
    DECLARE @closed TABLE (account_id INT);

    UPDATE dbo.Account
    SET status = 'closed'
    OUTPUT deleted.account_id INTO dbo.AuditLog (account_id)
    WHERE balance = 0;

    DELETE FROM dbo.Account
    OUTPUT deleted.account_id INTO @closed
    WHERE status = 'closed';

    SELECT account_id FROM @closed;
END;
GO

-- PIVOT over a real source: source captured by FROM, pivot internals are columns.
CREATE VIEW dbo.OrdersByYear
AS
SELECT account_id, [2023], [2024]
FROM (SELECT account_id, order_year, amount FROM dbo.Orders) src
PIVOT (SUM(amount) FOR order_year IN ([2023], [2024])) pvt;
GO

-- Qualified column references via alias resolution across a JOIN.
CREATE VIEW dbo.AccountOwners
AS
SELECT a.account_id, a.balance, p.full_name, p.region
FROM dbo.Account a
JOIN dbo.Person p ON a.person_id = p.person_id
WHERE a.status = 'active';
GO
