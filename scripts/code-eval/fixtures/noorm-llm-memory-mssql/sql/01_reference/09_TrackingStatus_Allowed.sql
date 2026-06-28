CREATE TABLE [dbo].[TrackingStatus_Allowed] (
    [from_status] VARCHAR(32) NOT NULL,
    [to_status] VARCHAR(32) NOT NULL,
    CONSTRAINT [PK_TrackingStatus_Allowed] PRIMARY KEY ([from_status], [to_status]),
    CONSTRAINT [FK_TrackingStatus_Allowed_From] FOREIGN KEY ([from_status]) REFERENCES [dbo].[TrackingStatus] ([tracking_status]) ON DELETE CASCADE,
    CONSTRAINT [FK_TrackingStatus_Allowed_To] FOREIGN KEY ([to_status]) REFERENCES [dbo].[TrackingStatus] ([tracking_status])
);
